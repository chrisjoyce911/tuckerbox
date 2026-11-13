package runs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/fleet"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
)

type fleetSession struct {
	claim          fleet.Claim
	tip            string
	mu             sync.Mutex
	lost           error
	cancel         context.CancelFunc
	done           chan struct{}
	store          fleet.Store
	branch         string
	fresh, durable bool
	prior          string
}

func (e *Engine) fleetStore() fleet.Store {
	return fleet.Store{Dir: e.Repo.Root, Remote: e.Config.Shipping.Remote, Lease: e.Config.Fleet.Lease, MaxRuns: e.Config.Fleet.MaxRuns, Now: e.Now}
}
func (e *Engine) claimFleet(ctx context.Context, req Request, sp StartPoint) error {
	if e.Config.Mode != config.ModeFleet {
		if req.TakeOver {
			return fmt.Errorf("--take-over requires fleet mode; in team mode close the merge request and delete its branch first")
		}
		return nil
	}
	if sp.Worktree == "" && req.Expect != nil && !req.Expect.NoRun {
		return &ChangedError{Change: req.Change, Why: "its run is gone"}
	}
	if sp.Worktree != "" && !req.TakeOver {
		ws, err := OpenWorkspace(ctx, sp.Worktree)
		if err != nil {
			return err
		}
		if _, err = ReadRun(ws.State.RunFile()); err == nil {
			return nil
		} else if !errors.Is(err, ErrNoRun) {
			return err
		}
		if e.gated(req) {
			if err = e.Gate(ctx, req.Change, sp); err != nil {
				return err
			}
		}
		if err = CheckOverrides(ctx, e.Config, e.Repo.Root, req.Change, sp); err != nil {
			return err
		}
	}
	if req.Mode != Start {
		return nil
	}
	if req.TakeOver {
		if sp.Worktree != "" || sp.Branch {
			return fmt.Errorf("take-over requires a fresh worktree and branch; leave the old worktree untouched")
		}
	}
	machine, err := ship.MachineName(e.Config.Machine)
	if err != nil {
		return err
	}
	store := e.fleetStore()
	store.Base = e.Config.Worktree.Base
	if req.TakeOver {
		if err := e.checkFleetMerged(ctx, req.Change); err != nil {
			return err
		}
	}
	got, err := store.Acquire(ctx, req.Change, machine, strconv.FormatInt(e.now().UnixNano(), 10), e.Repo.BranchName(e.Config.Worktree, req.Change), req.TakeOver, req.Force)
	if err != nil {
		return err
	}
	e.fleetRun = &fleetSession{claim: got.Claim, tip: got.BranchTip, store: store, branch: e.Repo.BranchName(e.Config.Worktree, req.Change), fresh: true}
	if req.TakeOver {
		return e.checkFleetMerged(ctx, req.Change)
	}
	return nil
}
func (e *Engine) activateFleet(ctx context.Context, req Request, ws Workspace, rs *Run) error {
	if e.Config.Mode != config.ModeFleet {
		return nil
	}
	if e.fleetRun != nil {
		c := e.fleetRun.claim
		rs.FleetClaim = &c
		rs.FleetRemote = e.Config.Shipping.Remote
		// Preserve the fleet run id through the existing ship identity format.
		ns, err := strconv.ParseInt(c.Run, 10, 64)
		if err != nil {
			return err
		}
		rs.Created = time.Unix(0, ns)
		if e.fleetRun.tip != "" {
			rs.LastPush = e.fleetRun.tip
			rs.ShippingStarted = true
			rs.Step = ""
		}
		return nil
	}
	if rs.FleetClaim == nil {
		return fmt.Errorf("run has no fleet claim; release any previous claim and start a fresh run")
	}
	machine, err := ship.MachineName(e.Config.Machine)
	if err != nil {
		return err
	}
	if rs.FleetClaim.Machine != machine {
		return fmt.Errorf("claimed by %s", rs.FleetClaim.Machine)
	}
	// prepare has judged the request but hasn't saved it: a full limit leaves
	// both persisted run and claim ready. Conditional Expect was checked first.
	store := e.fleetStore()
	store.Base = rs.Base
	store.Remote = rs.FleetRemote
	previous, err := store.Transition(ctx, *rs.FleetClaim, fleet.Active)
	if err != nil {
		return err
	}
	e.fleetRun = &fleetSession{claim: *rs.FleetClaim, store: store, prior: previous.State}
	return nil
}
func (e *Engine) fleetFence(rs *Run) *fleet.Fence {
	if rs.FleetClaim == nil || e.Config.Mode != config.ModeFleet {
		return nil
	}
	store := e.fleetStore()
	store.Remote = rs.FleetRemote
	store.Base = rs.Base
	return &fleet.Fence{Store: store, Claim: *rs.FleetClaim}
}
func (e *Engine) fleetEpoch(rs *Run) uint64 {
	if rs.FleetClaim != nil {
		return rs.FleetClaim.Epoch
	}
	return 0
}
func (e *Engine) watchFleet(ctx context.Context, cancelRun context.CancelFunc, rs *Run) context.Context {
	f := e.fleetRun
	if f == nil {
		return ctx
	}
	watch, cancel := context.WithCancel(ctx)
	f.cancel = cancel
	f.done = make(chan struct{})
	lease := e.Config.Fleet.Lease
	if lease <= 0 {
		lease = 10 * time.Minute
	}
	fence := e.fleetFence(rs)
	lastRenewal := e.now()
	go func() {
		defer close(f.done)
		ticks := e.FleetTicks
		var ticker *time.Ticker
		if ticks == nil {
			ticker = time.NewTicker(max(lease/5, time.Nanosecond))
			defer ticker.Stop()
			ticks = ticker.C
		}
		for {
			select {
			case <-watch.Done():
				return
			case <-ticks:
				err := e.renewFleet(watch, fence)
				if err != nil {
					if watch.Err() != nil {
						return
					}
					if !fleet.OwnershipLost(err) && e.now().Before(lastRenewal.Add(lease)) {
						e.logf(rs.Change, "fleet renewal unavailable; retrying: %v", err)
						continue
					}
					f.mu.Lock()
					f.lost = err
					f.mu.Unlock()
					cancelRun()
					return
				}
				lastRenewal = e.now()
			}
		}
	}()
	return ctx
}
func (e *Engine) stopFleet() {
	if f := e.fleetRun; f != nil && f.cancel != nil {
		f.cancel()
		<-f.done
		f.cancel = nil
	}
}
func (e *Engine) finishFleet(rs *Run, state State, reason string) (State, string) {
	if e.Config.Mode != config.ModeFleet || rs.FleetClaim == nil {
		return state, reason
	}
	e.stopFleet()
	if f := e.fleetRun; f != nil {
		f.mu.Lock()
		lost := f.lost
		f.mu.Unlock()
		if lost != nil && state != Merged {
			state, reason = Blocked, lost.Error()
			if fleet.OwnershipLost(lost) {
				return state, reason
			}
		}
	}
	fence := e.fleetFence(rs)
	err := e.settleFleet(func(ctx context.Context) error {
		switch state {
		case Merged:
			if e.fleetRemove != nil {
				return e.fleetRemove(ctx, fence)
			}
			return fence.Store.RemoveMerged(ctx, fence.Claim)
		case Ready:
			return e.setFleetState(ctx, fence, fleet.Ready)
		case Stopped, Blocked, Failed:
			return e.setFleetState(ctx, fence, fleet.Parked)
		}
		return nil
	})
	if err != nil {
		if state == Merged {
			e.logf(rs.Change, "merged; fleet cleanup: %v", err)
			return state, reason
		}
		return Blocked, err.Error()
	}
	return state, reason
}

// Startup changes are committed only once the first run.json write succeeds.
// Rollback runs before releasing the local change lock, and checks the full
// identity before restoring state or removing a freshly acquired claim.
func (e *Engine) rollbackFleet() error {
	f := e.fleetRun
	if f == nil || f.durable {
		return nil
	}
	err := e.settleFleet(func(ctx context.Context) error {
		if f.fresh {
			return f.store.Abandon(ctx, f.claim, f.branch)
		}
		return f.store.SetState(ctx, f.claim, f.prior)
	})
	if fleet.OwnershipLost(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("rolling back fleet startup: %w", err)
	}
	return nil
}
func (e *Engine) renewFleet(ctx context.Context, f *fleet.Fence) error {
	if e.fleetRenew != nil {
		return e.fleetRenew(ctx, f)
	}
	return f.Renew(ctx)
}
func (e *Engine) setFleetState(ctx context.Context, f *fleet.Fence, state string) error {
	if e.fleetSetState != nil {
		return e.fleetSetState(ctx, f, state)
	}
	return f.Store.SetState(ctx, f.Claim, state)
}
func (e *Engine) fleetPause(ctx context.Context, d time.Duration) error {
	if e.fleetWait != nil {
		return e.fleetWait(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// A transport outage is not an ownership decision. Keep the existing process
// and change lock until the remote accepts the terminal claim state. If the
// process is killed meanwhile this is a crash, not a falsely parked run.
func (e *Engine) settleFleet(action func(context.Context) error) error {
	delay := time.Second
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := action(ctx)
		cancel()
		if err == nil || !fleet.Temporary(err) {
			return err
		}
		if err = e.fleetPause(context.Background(), delay); err != nil {
			return err
		}
		delay = min(delay*2, 30*time.Second)
	}
}
func (e *Engine) checkFleetMerged(ctx context.Context, change string) error {
	if e.Config.Ship == config.ShipLocal || e.Config.Ship == "" {
		return nil
	}
	client, _, err := e.shippingClient(ctx)
	if err != nil {
		return err
	}
	history, ok := client.(interface {
		FindMergedMR(context.Context, string) (*forge.MR, error)
	})
	if !ok {
		return fmt.Errorf("cannot confirm whether %s is already merged: forge history is unavailable", change)
	}
	mr, err := history.FindMergedMR(ctx, e.Repo.BranchName(e.Config.Worktree, change))
	if err != nil {
		return err
	}
	if mr != nil {
		if err := e.settleFleet(func(ctx context.Context) error { return e.fleetStore().RecordMerged(ctx, change) }); err != nil {
			return err
		}
		return fmt.Errorf("change %s is already merged: %s", change, mr.URL)
	}
	return nil
}

func (e *Engine) takeoverWorkspace(ctx context.Context, req Request, tip string) (Workspace, error) {
	path := e.Repo.WorktreePath(e.Config.Worktree, req.Change)
	branch := e.Repo.BranchName(e.Config.Worktree, req.Change)
	if _, err := os.Stat(path); err == nil {
		return Workspace{}, fmt.Errorf("take-over worktree already exists")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return Workspace{}, err
	}
	if _, err := git(ctx, e.Repo.Root, "worktree", "add", "--no-track", "-b", branch, path, tip); err != nil {
		return Workspace{}, err
	}
	ws, err := OpenWorkspace(ctx, path)
	ws.Created = true
	ws.Base, _ = e.Repo.Base(ctx, e.Config.Worktree.Base, nil)
	return ws, err
}
