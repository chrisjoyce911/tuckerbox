package runs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/fleet"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

func TestFleetStartupFailureReleasesFreshClaim(t *testing.T) {
	for _, at := range []string{"worktree", "second gate", "prepare", "pid file"} {
		t.Run(at, func(t *testing.T) {
			f := fleetSetup(t)
			e := f.engine()
			switch at {
			case "worktree":
				testenv.WriteFile(t, f.worktree("add-login"), "existing user file")
			case "second gate":
				e.Gate = func(_ context.Context, _ string, sp StartPoint) error {
					if sp.Worktree != "" {
						return errors.New("approval withdrawn")
					}
					return nil
				}
			case "prepare":
				e.Config.Steps = nil
			case "pid file":
				e.Gate = func(ctx context.Context, _ string, sp StartPoint) error {
					if sp.Worktree != "" {
						ws, err := OpenWorkspace(ctx, sp.Worktree)
						if err != nil {
							return err
						}
						return os.MkdirAll(ws.State.PidFile(), 0755)
					}
					return nil
				}
			}
			_, err := e.Run(context.Background(), Request{Change: "add-login", Scope: "1"})
			if err == nil {
				t.Fatal("expected startup failure")
			}
			all, err := f.fleetStore().Read(context.Background())
			if err != nil || len(all) != 0 {
				t.Fatalf("leaked fresh claim: %v %v", all, err)
			}
			if at == "second gate" {
				f.env.SetPlan(t, map[string]testenv.Action{"build": tick("1.1", "1.2")})
				r := f.run(Request{Change: "add-login", Scope: "1"})
				if r.State != Ready {
					t.Fatal("refused start could not retry", r)
				}
			}
		})
	}
}
func TestFleetStartupFailureRestoresPriorClaimState(t *testing.T) {
	for _, prior := range []State{Ready, Blocked} {
		t.Run(string(prior), func(t *testing.T) {
			f := fleetSetup(t)
			if prior == Ready {
				f.env.SetPlan(t, map[string]testenv.Action{"build": tick("1.1", "1.2")})
			} else {
				f.env.SetPlan(t, map[string]testenv.Action{"build": {Status: "blocked", Summary: "question"}})
			}
			old := f.run(Request{Change: "add-login", Scope: "1"})
			e := f.engine()
			e.Gate = func(ctx context.Context, _ string, sp StartPoint) error {
				ws, err := OpenWorkspace(ctx, sp.Worktree)
				if err != nil {
					return err
				}
				return os.MkdirAll(ws.State.PidFile(), 0755)
			}
			_, err := e.Run(context.Background(), Request{Change: "add-login", Scope: "2"})
			if err == nil {
				t.Fatal("expected pid failure")
			}
			all, err := f.fleetStore().Read(context.Background())
			want := fleet.Ready
			if prior == Blocked {
				want = fleet.Parked
			}
			if err != nil || all["add-login"].State != want || all["add-login"].Epoch != old.FleetClaim.Epoch {
				t.Fatalf("prior claim lost: %v %v", all, err)
			}
			if f.current("add-login").State != prior {
				t.Fatal("failed restart changed saved run")
			}
		})
	}
}
func TestFleetTakeoverStartupFailureReleasesWithBranchFence(t *testing.T) {
	f := fleetSetup(t)
	bare := testenv.Git(t, f.dir, "remote", "get-url", "origin")
	dir := filepath.Join(t.TempDir(), "old")
	testenv.Git(t, f.dir, "clone", bare, dir)
	testenv.Git(t, dir, "config", "user.name", "Test")
	testenv.Git(t, dir, "config", "user.email", "test@example.invalid")
	testenv.Git(t, dir, "checkout", "-b", "add-login")
	testenv.Git(t, dir, "commit", "--allow-empty", "-m", "change")
	head := testenv.Git(t, dir, "rev-parse", "HEAD")
	now := time.Now()
	store := fleet.Store{Dir: dir, Now: func() time.Time { return now }}
	got, err := store.Acquire(context.Background(), "add-login", "old", "run", "add-login", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Push(context.Background(), got.Claim, "add-login", "", head); err != nil {
		t.Fatal(err)
	}
	testenv.WriteFile(t, f.worktree("add-login"), "user file prevents worktree creation")
	e := f.engine()
	e.Now = func() time.Time { return now.Add(time.Hour) }
	_, err = e.Run(context.Background(), Request{Change: "add-login", TakeOver: true})
	if err == nil {
		t.Fatal("expected worktree failure")
	}
	all, _ := store.Read(context.Background())
	if len(all) != 0 {
		t.Fatal("takeover claim leaked", all)
	}
	tip := testenv.Git(t, bare, "rev-parse", "refs/heads/add-login")
	subject := testenv.Git(t, bare, "show", "-s", "--format=%s", tip)
	if !strings.HasPrefix(subject, "Release add-login from ") || tip == head {
		t.Fatalf("rollback not fenced: %s %s", tip, subject)
	}
	takeoverParent := testenv.Git(t, bare, "rev-parse", tip+"^")
	if testenv.Git(t, bare, "rev-parse", takeoverParent+"^") != head {
		t.Fatal("rollback lost takeover history")
	}
}
func temporaryFleetError() error {
	return &fleet.TransportError{Err: errors.New("temporary network outage")}
}
func TestFleetTransientRenewalRetriesWithoutStoppingAgent(t *testing.T) {
	f := fleetSetup(t)
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: filepath.Join(t.TempDir(), "never")}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := f.engine()
	ticks := make(chan time.Time, 2)
	e.FleetTicks = ticks
	events := make(chan int, 2)
	n := 0
	e.fleetRenew = func(ctx context.Context, fence *fleet.Fence) error {
		n++
		if n == 1 {
			events <- n
			return temporaryFleetError()
		}
		err := fence.Renew(ctx)
		events <- n
		return err
	}
	done := make(chan result, 1)
	go func() { r, err := e.Run(ctx, Request{Change: "add-login"}); done <- result{r, err} }()
	waitFor(t, "agent started", func() bool { r := f.current("add-login"); return r != nil && r.AgentPid > 0 })
	ticks <- time.Now()
	<-events
	ticks <- time.Now()
	if <-events != 2 {
		t.Fatal("renewal did not retry")
	}
	select {
	case out := <-done:
		t.Fatal("transient renewal stopped run", out)
	default:
	}
	cancel()
	out := <-done
	if out.err != nil || out.run.State != Stopped {
		t.Fatalf("%+v %v", out.run, out.err)
	}
	all, _ := f.fleetStore().Read(context.Background())
	if all["add-login"].State != fleet.Parked {
		t.Fatal(all)
	}
}
func TestFleetRenewalDeadlineBlocksOnlyAfterClaimIsParked(t *testing.T) {
	f := fleetSetup(t)
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: filepath.Join(t.TempDir(), "never")}})
	e := f.engine()
	base := time.Now()
	var clock atomic.Int64
	clock.Store(base.UnixNano())
	e.Now = func() time.Time { return time.Unix(0, clock.Load()) }
	ticks := make(chan time.Time, 1)
	e.FleetTicks = ticks
	e.fleetRenew = func(context.Context, *fleet.Fence) error { return temporaryFleetError() }
	var attempts int
	e.fleetSetState = func(ctx context.Context, fence *fleet.Fence, state string) error {
		attempts++
		if attempts == 1 {
			return temporaryFleetError()
		}
		return fence.Store.SetState(ctx, fence.Claim, state)
	}
	e.fleetWait = func(context.Context, time.Duration) error { return nil }
	done := make(chan result, 1)
	go func() { r, err := e.Run(context.Background(), Request{Change: "add-login"}); done <- result{r, err} }()
	waitFor(t, "agent started", func() bool { r := f.current("add-login"); return r != nil && r.AgentPid > 0 })
	clock.Store(base.Add(time.Hour).UnixNano())
	ticks <- base.Add(time.Hour)
	out := <-done
	if out.err != nil || out.run.State != Blocked || attempts < 2 {
		t.Fatalf("%+v %v attempts%d", out.run, out.err, attempts)
	}
	all, _ := f.fleetStore().Read(context.Background())
	if all["add-login"].State != fleet.Parked {
		t.Fatal("blocked claim stayed active", all)
	}
	store := f.fleetStore()
	store.Now = func() time.Time { return base.Add(48 * time.Hour) }
	_, err := store.Acquire(context.Background(), "add-login", "other", "new", "add-login", true, false)
	fleetErr(t, err, "--force")
}
func TestFleetMergedCleanupRetriesWithoutChangingMergedState(t *testing.T) {
	f, e, original := shippingSetup(t, config.ShipMerge, false, "add-login")
	f.cfg.Mode = config.ModeFleet
	bare := testenv.Git(t, f.dir, "remote", "get-url", "origin")
	api := &fleetShippingForge{shippingForge: original, bare: bare}
	e.Forge = func(context.Context) (forge.Client, func(string) string, error) { return api, nil, nil }
	attempts := 0
	e.fleetRemove = func(ctx context.Context, fence *fleet.Fence) error {
		attempts++
		if attempts == 1 {
			return temporaryFleetError()
		}
		return fence.Store.RemoveMerged(ctx, fence.Claim)
	}
	e.fleetWait = func(context.Context, time.Duration) error { return nil }
	r, err := e.Run(context.Background(), Request{Change: "add-login"})
	if err != nil || r.State != Merged || attempts != 2 {
		t.Fatalf("%+v %v attempts%d", r, err, attempts)
	}
	all, _ := f.fleetStore().Read(context.Background())
	if len(all) != 0 {
		t.Fatal(all)
	}
	_, err = f.fleetStore().Acquire(context.Background(), "add-login", "other", "new", "add-login", false, false)
	fleetErr(t, err, "already merged")
}
func TestFleetTakeoverRefusesMergedForgeHistoryWithoutArchive(t *testing.T) {
	for _, branchExists := range []bool{false, true} {
		t.Run(fmt.Sprint(branchExists), func(t *testing.T) {
			f, e, original := shippingSetup(t, config.ShipMR, false, "add-login")
			f.cfg.Mode = config.ModeFleet
			bare := testenv.Git(t, f.dir, "remote", "get-url", "origin")
			now := time.Now()
			store := f.fleetStore()
			store.Now = func() time.Time { return now }
			_, err := store.Acquire(context.Background(), "add-login", "old", "run", "add-login", false, false)
			if err != nil {
				t.Fatal(err)
			}
			if branchExists {
				testenv.Git(t, f.dir, "push", "origin", "main:refs/heads/add-login")
			}
			original.mr = &forge.MR{Number: 7, State: "merged", URL: "https://forge.example/mr/7"}
			api := &fleetShippingForge{shippingForge: original, bare: bare}
			e.Now = func() time.Time { return now.Add(time.Hour) }
			e.Forge = func(context.Context) (forge.Client, func(string) string, error) { return api, nil, nil }
			_, err = e.Run(context.Background(), Request{Change: "add-login", TakeOver: true})
			fleetErr(t, err, "already merged")
			if _, err = os.Stat(f.worktree("add-login")); !os.IsNotExist(err) {
				t.Fatal("merged takeover made a worktree")
			}
			all, _ := store.Read(context.Background())
			if len(all) != 0 {
				t.Fatal("merged claim was not cleaned up", all)
			}
			_, err = store.Acquire(context.Background(), "add-login", "new", "again", "add-login", false, false)
			fleetErr(t, err, "already merged")
			if original.opened != 0 {
				t.Fatal("opened a second MR")
			}
		})
	}
}

func TestFleetReadyTransitionRetriesTransportFailure(t *testing.T) {
	f := fleetSetup(t)
	f.env.SetPlan(t, map[string]testenv.Action{"build": tick("1.1", "1.2")})
	e := f.engine()
	attempts := 0
	e.fleetSetState = func(ctx context.Context, fence *fleet.Fence, state string) error {
		attempts++
		if attempts == 1 {
			return temporaryFleetError()
		}
		return fence.Store.SetState(ctx, fence.Claim, state)
	}
	e.fleetWait = func(context.Context, time.Duration) error { return nil }
	r, err := e.Run(context.Background(), Request{Change: "add-login", Scope: "1"})
	if err != nil || r.State != Ready || attempts != 2 {
		t.Fatalf("%+v %v attempts%d", r, err, attempts)
	}
	all, _ := f.fleetStore().Read(context.Background())
	if all["add-login"].State != fleet.Ready {
		t.Fatal(all)
	}
}

type mergeBetweenChecksForge struct {
	*fleetShippingForge
	calls int
}

func (f *mergeBetweenChecksForge) FindMergedMR(context.Context, string) (*forge.MR, error) {
	f.calls++
	if f.calls == 1 {
		return nil, nil
	}
	return &forge.MR{State: "merged", Number: 7, URL: "https://forge.example/mr/7"}, nil
}
func TestFleetTakeoverRechecksMergeAfterFencingBranch(t *testing.T) {
	f, e, original := shippingSetup(t, config.ShipMR, false, "add-login")
	f.cfg.Mode = config.ModeFleet
	bare := testenv.Git(t, f.dir, "remote", "get-url", "origin")
	now := time.Now()
	store := f.fleetStore()
	store.Now = func() time.Time { return now }
	got, err := store.Acquire(context.Background(), "add-login", "old", "run", "add-login", false, false)
	if err != nil {
		t.Fatal(err)
	}
	tree := testenv.Git(t, f.dir, "rev-parse", "HEAD^{tree}")
	base := testenv.Git(t, f.dir, "rev-parse", "HEAD")
	head := testenv.Git(t, f.dir, "commit-tree", tree, "-p", base, "-m", "unarchived work")
	if err = store.Push(context.Background(), got.Claim, "add-login", "", head); err != nil {
		t.Fatal(err)
	}
	api := &mergeBetweenChecksForge{fleetShippingForge: &fleetShippingForge{shippingForge: original, bare: bare}}
	e.Now = func() time.Time { return now.Add(time.Hour) }
	e.Forge = func(context.Context) (forge.Client, func(string) string, error) { return api, nil, nil }
	_, err = e.Run(context.Background(), Request{Change: "add-login", TakeOver: true})
	fleetErr(t, err, "already merged")
	if api.calls != 2 || original.opened != 0 {
		t.Fatalf("calls%d opens%d", api.calls, original.opened)
	}
	all, _ := store.Read(context.Background())
	if len(all) != 0 {
		t.Fatal("merge race left active claim", all)
	}
	_, err = store.Acquire(context.Background(), "add-login", "other", "new", "add-login", false, false)
	fleetErr(t, err, "already merged")
	if _, err = os.Stat(f.worktree("add-login")); !os.IsNotExist(err) {
		t.Fatal("ran workflow after merge race")
	}
}
