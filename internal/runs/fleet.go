package runs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/fleet"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
)

type fleetSession struct {
	claim  fleet.Claim
	tip    string
	mu     sync.Mutex
	lost   error
	cancel context.CancelFunc
	done   chan struct{}
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
	if sp.Worktree != "" && !req.TakeOver {
		return nil
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
	store.Base = sp.Ref
	got, err := store.Acquire(ctx, req.Change, machine, strconv.FormatInt(e.now().UnixNano(), 10), e.Repo.BranchName(e.Config.Worktree, req.Change), req.TakeOver, req.Force)
	if err != nil {
		return err
	}
	e.fleetRun = &fleetSession{claim: got.Claim, tip: got.BranchTip}
	return nil
}
func (e *Engine) activateFleet(ctx context.Context, req Request, ws Workspace, rs *Run) error {
	if e.Config.Mode != config.ModeFleet {
		return nil
	}
	if e.fleetRun != nil {
		c := e.fleetRun.claim
		rs.FleetClaim = &c
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
	if err = store.SetState(ctx, *rs.FleetClaim, fleet.Active); err != nil {
		return err
	}
	e.fleetRun = &fleetSession{claim: *rs.FleetClaim}
	return nil
}
func (e *Engine) fleetFence(rs *Run) *fleet.Fence {
	if rs.FleetClaim == nil || e.Config.Mode != config.ModeFleet {
		return nil
	}
	return &fleet.Fence{Store: e.fleetStore(), Claim: *rs.FleetClaim}
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
				if err := e.fleetStore().Renew(watch, f.claim); err != nil {
					if watch.Err() != nil {
						return
					}
					f.mu.Lock()
					f.lost = err
					f.mu.Unlock()
					cancelRun()
					return
				}
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
		if lost != nil {
			return Blocked, lost.Error()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store := e.fleetStore()
	store.Base = rs.Base
	var err error
	switch state {
	case Merged:
		err = store.RemoveMerged(ctx, *rs.FleetClaim)
	case Ready:
		err = store.SetState(ctx, *rs.FleetClaim, fleet.Ready)
	case Stopped, Blocked, Failed:
		err = store.SetState(ctx, *rs.FleetClaim, fleet.Parked)
	}
	if err != nil {
		return Blocked, err.Error()
	}
	return state, reason
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
