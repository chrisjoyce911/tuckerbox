package runs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/fleet"
	"github.com/chrisjoyce911/tuckerbox/internal/fleetslot"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

func fleetSetup(t *testing.T) *fixture {
	t.Helper()
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login", "add-users"}})
	f.withTasks(scopedTasks, "add-login", "add-users")
	bare := filepath.Join(t.TempDir(), "remote.git")
	testenv.Git(t, f.dir, "clone", "--bare", f.dir, bare)
	testenv.Git(t, f.dir, "remote", "add", "origin", bare)
	f.cfg.Mode = config.ModeFleet
	f.cfg.Machine = "one"
	f.cfg.Fleet.Lease = 10 * time.Minute
	return f
}
func (f *fixture) fleetStore() fleet.Store {
	return fleet.Store{Dir: f.dir, Lease: f.cfg.Fleet.Lease, MaxRuns: f.cfg.Fleet.MaxRuns}
}
func fleetErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}
func TestFleetClaimRefusesBeforeWorktreeAndAfterGate(t *testing.T) {
	f := fleetSetup(t)
	s := f.fleetStore()
	if _, err := s.Acquire(context.Background(), "add-login", "two", "foreign", "add-login", false, false); err != nil {
		t.Fatal(err)
	}
	e := f.engine()
	gated := false
	e.Gate = func(context.Context, string, StartPoint) error { gated = true; return nil }
	_, err := e.Run(context.Background(), Request{Change: "add-login"})
	fleetErr(t, err, "claimed by two")
	if !gated {
		t.Fatal("claim attempted before gate")
	}
	if _, err := os.Stat(f.worktree("add-login")); !os.IsNotExist(err) {
		t.Fatal("losing claim created worktree")
	}
}
func TestFleetNextScopeAdmissionAndRestOfChange(t *testing.T) {
	for _, scope := range []string{"2", ""} {
		t.Run("scope="+scope, func(t *testing.T) {
			f := fleetSetup(t)
			f.cfg.Fleet.MaxRuns = 1
			f.env.SetPlan(t, map[string]testenv.Action{"build": tick("1.1", "1.2")})
			r := f.run(Request{Change: "add-login", Scope: "1"})
			if r.State != Ready || r.FleetClaim == nil {
				t.Fatal(r)
			}
			first := r.FleetClaim
			store := f.fleetStore()
			other, err := store.Acquire(context.Background(), "add-users", "two", "run2", "add-users", false, false)
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.runErr(Request{Change: "add-login", Scope: scope})
			fleetErr(t, err, "fleet limit reached: add-users")
			if f.current("add-login").State != Ready {
				t.Fatal("refused scope changed run")
			}
			all, _ := store.Read(context.Background())
			if all["add-login"].State != fleet.Ready {
				t.Fatal("refused scope changed claim")
			}
			if err = store.SetState(context.Background(), other.Claim, fleet.Ready); err != nil {
				t.Fatal(err)
			}
			f.env.SetPlan(t, map[string]testenv.Action{"build": tick("2.1", "2.2")})
			r = f.run(Request{Change: "add-login", Scope: scope})
			if r.State != Ready || r.FleetClaim.Epoch != first.Epoch || r.Created.UnixNano() != firstCreated(first) {
				t.Fatalf("claim not reused: %+v", r)
			}
		})
	}
}
func firstCreated(c *fleet.Claim) int64 {
	var ns int64
	for _, b := range c.Run {
		ns = ns*10 + int64(b-'0')
	}
	return ns
}
func TestFleetParkAndNextScopeAfterBlock(t *testing.T) {
	f := fleetSetup(t)
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Status: "blocked", Summary: "question"}})
	r := f.run(Request{Change: "add-login", Scope: "1"})
	if r.State != Blocked {
		t.Fatal(r)
	}
	all, _ := f.fleetStore().Read(context.Background())
	if all["add-login"].State != fleet.Parked {
		t.Fatal(all)
	}
	f.env.SetPlan(t, map[string]testenv.Action{"build": tick("2.1", "2.2")})
	next := f.run(Request{Change: "add-login", Scope: "2"})
	if next.State != Ready || next.FleetClaim.Epoch != r.FleetClaim.Epoch {
		t.Fatal(next)
	}
}
func TestFleetScopeOnAnotherMachine(t *testing.T) {
	f := fleetSetup(t)
	f.env.SetPlan(t, map[string]testenv.Action{"build": tick("1.1", "1.2")})
	f.run(Request{Change: "add-login", Scope: "1"})
	f.cfg.Machine = "two"
	_, err := f.runErr(Request{Change: "add-login", Scope: "2"})
	fleetErr(t, err, "claimed by one")
}
func TestFleetLostClaimStopsAgentFromFakeClockTick(t *testing.T) {
	f := fleetSetup(t)
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: filepath.Join(t.TempDir(), "never")}})
	e := f.engine()
	ticks := make(chan time.Time, 1)
	e.FleetTicks = ticks
	finished := make(chan result, 1)
	go func() {
		r, err := e.Run(context.Background(), Request{Change: "add-login"})
		finished <- resultValue(r, err)
	}()
	f.waitState("add-login", Running, "build")
	r := f.current("add-login")
	waitFor(t, "agent started", func() bool { return f.current("add-login").AgentPid > 0 })
	store := f.fleetStore()
	store.Now = func() time.Time { return r.Created.Add(time.Hour) }
	if _, err := store.Acquire(context.Background(), "add-login", "two", "new", "add-login", true, false); err != nil {
		t.Fatal(err)
	}
	ticks <- r.Created.Add(time.Hour)
	select {
	case out := <-finished:
		if out.err != nil || out.run.State != Blocked || !strings.Contains(out.run.Reason, "claim taken by two") || out.run.AgentPid != 0 {
			t.Fatalf("%+v %v", out.run, out.err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("claim loss did not stop agent")
	}
}
func resultValue(r *Run, err error) result { return result{r, err} }
func TestFleetDispatchedNextScopeTakesSlotBeforeClaim(t *testing.T) {
	f := fleetSetup(t)
	f.cfg.FleetCapacity = 1
	f.env.SetPlan(t, map[string]testenv.Action{"build": tick("1.1", "1.2")})
	r := f.run(Request{Change: "add-login", Scope: "1", Dispatched: true})
	if !r.Dispatched {
		t.Fatal("dispatch flag not persisted")
	}
	slot, err := fleetslot.Take(f.env.StateHome, "one", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer slot.Free()
	_, err = f.runErr(Request{Change: "add-login", Scope: "2"})
	fleetErr(t, err, "one is full")
	all, _ := f.fleetStore().Read(context.Background())
	if all["add-login"].State != fleet.Ready {
		t.Fatal("full machine changed claim")
	}
}
func TestFleetExpectMismatchDoesNotReactivateClaim(t *testing.T) {
	f := fleetSetup(t)
	f.env.SetPlan(t, map[string]testenv.Action{"build": tick("1.1", "1.2")})
	r := f.run(Request{Change: "add-login", Scope: "1"})
	_, err := f.runErr(Request{Change: "add-login", Scope: "2", Expect: &Expect{State: Ready, Since: r.Since.Add(-time.Second)}})
	fleetErr(t, err, "changed since")
	all, _ := f.fleetStore().Read(context.Background())
	if all["add-login"].State != fleet.Ready {
		t.Fatal("conditional mismatch changed claim")
	}
}

func TestFleetModeChangeKeepsExistingClaimProtocol(t *testing.T) {
	f := fleetSetup(t)
	f.env.SetPlan(t, map[string]testenv.Action{"build": tick("1.1", "1.2")})
	r := f.run(Request{Change: "add-login", Scope: "1"})
	if r.Mode != config.ModeFleet {
		t.Fatal("started mode not recorded")
	}
	f.cfg.Mode = config.ModeSolo
	other := f.fleetStore()
	other.Now = func() time.Time { return r.Created.Add(time.Hour) }
	if err := other.Release(context.Background(), "add-login", "two", "add-login", true); err != nil {
		t.Fatal(err)
	}
	_, err := f.runErr(Request{Change: "add-login", Scope: "2"})
	fleetErr(t, err, "claim released")
	if f.current("add-login").State != Ready {
		t.Fatal("mode change bypassed old claim protocol")
	}
}

func TestFleetMissingExpectedRunDoesNotClaimOrCreateWorktree(t *testing.T) {
	f := fleetSetup(t)
	_, err := f.runErr(Request{Change: "add-login", Expect: &Expect{State: Ready, Since: time.Now()}})
	fleetErr(t, err, "its run is gone")
	all, _ := f.fleetStore().Read(context.Background())
	if len(all) != 0 {
		t.Fatal("conditional mismatch wrote claim")
	}
	if _, err := os.Stat(f.worktree("add-login")); !os.IsNotExist(err) {
		t.Fatal("conditional mismatch made worktree")
	}
}

type fleetShippingForge struct {
	*shippingForge
	bare string
}

func (f *fleetShippingForge) UpdateMR(_ context.Context, _ int, r forge.OpenRequest) (*forge.MR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mr.Body = r.Body
	f.mr.Title = r.Title
	m := *f.mr
	return &m, nil
}
func (f *fleetShippingForge) GetMR(ctx context.Context, n int) (*forge.MR, error) {
	f.mu.Lock()
	m := *f.mr
	f.mu.Unlock()
	out, err := exec.CommandContext(ctx, "git", "-C", f.bare, "rev-parse", "refs/heads/add-login").Output()
	if err != nil {
		return nil, err
	}
	m.Head = strings.TrimSpace(string(out))
	m.Mergeable = true
	m.MergeStatus = "clean"
	return &m, nil
}
func (f *fleetShippingForge) Merge(ctx context.Context, n int, sha, method string) error {
	m, err := f.GetMR(ctx, n)
	if err != nil {
		return err
	}
	if m.Head != sha {
		return &forge.APIError{Status: 409, Message: "head moved"}
	}
	return f.shippingForge.Merge(ctx, n, sha, method)
}
func TestFleetTakeoverResumesArchivedShippingWithoutSteps(t *testing.T) {
	f, e, original := shippingSetup(t, config.ShipMR, true, "add-login")
	f.cfg.Mode = config.ModeFleet
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.FixedZone("AEST", 10*3600))
	e.Now = func() time.Time { return now }
	bare := testenv.Git(t, f.dir, "remote", "get-url", "origin")
	api := &fleetShippingForge{shippingForge: original, bare: bare}
	e.Forge = func(context.Context) (forge.Client, func(string) string, error) { return api, nil, nil }
	r, err := e.Run(context.Background(), Request{Change: "add-login"})
	if err != nil || r.State != Ready {
		t.Fatalf("old run: %+v %v", r, err)
	}
	if !r.ArchiveDone {
		t.Fatal("original did not archive")
	}
	oldTip := r.LastPush
	store := f.fleetStore()
	store.Now = e.Now
	// Keep the remote in the state of a holder that died while waiting for CI.
	if err = store.SetState(context.Background(), *r.FleetClaim, fleet.Active); err != nil {
		t.Fatal(err)
	}
	otherDir := filepath.Join(t.TempDir(), "other")
	testenv.Git(t, f.dir, "clone", bare, otherDir)
	testenv.Git(t, otherDir, "config", "user.name", "Test")
	testenv.Git(t, otherDir, "config", "user.email", "test@example.invalid")
	other := open(t, f.env, otherDir)
	cfg := *f.cfg
	cfg.Machine = "mini"
	other.cfg = &cfg
	next := other.engine()
	next.Now = func() time.Time { return now.Add(time.Hour) }
	next.Forge = e.Forge
	gateCalls := 0
	next.Gate = func(_ context.Context, _ string, at StartPoint) error {
		gateCalls++
		if at.Worktree != "" {
			return fmt.Errorf("takeover must gate from base, not archived worktree")
		}
		return nil
	}
	recovered, err := next.Run(context.Background(), Request{Change: "add-login", TakeOver: true})
	if err != nil || recovered.State != Ready {
		t.Fatalf("takeover: %+v %v", recovered, err)
	}
	if gateCalls != 1 || len(recovered.History) != 0 || recovered.FleetClaim.Epoch != r.FleetClaim.Epoch+1 || recovered.ArchivePath == "" || recovered.LastPush == oldTip {
		t.Fatalf("takeover reran work: %+v gates=%d", recovered, gateCalls)
	}
	if _, err := os.Stat(filepath.Join(r.Worktree, "login.go")); err != nil {
		t.Fatal("old worktree modified")
	}
	if original.opened != 1 {
		t.Fatal("second MR opened")
	}
	if _, err = e.fleetFence(r).Store.Read(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := (ship.Git{Worktree: r.Worktree, Remote: "origin", Branch: r.Branch, Base: r.Base, Fleet: e.fleetFence(r)}).Push(context.Background(), oldTip); err == nil {
		t.Fatal("old holder pushed after recovery")
	}
	if err = api.Merge(context.Background(), r.MR.Number, oldTip, "merge"); err == nil {
		t.Fatal("old holder merged after recovery")
	}
}

func TestFleetResumeAndRerunReactivateParkedClaim(t *testing.T) {
	for _, mode := range []Mode{Resume, Rerun} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			f := fleetSetup(t)
			f.env.SetPlan(t, map[string]testenv.Action{"build": {Status: "blocked", Summary: "question"}})
			r := f.run(Request{Change: "add-login", Scope: "1"})
			if r.State != Blocked {
				t.Fatal(r)
			}
			f.env.SetPlan(t, map[string]testenv.Action{"build": tick("1.1", "1.2")})
			r = f.run(Request{Change: "add-login", Mode: mode, Step: "build", Worktree: r.Worktree})
			if r.State != Ready {
				t.Fatal(r)
			}
			all, _ := f.fleetStore().Read(context.Background())
			if all["add-login"].State != fleet.Ready {
				t.Fatal(all)
			}
		})
	}
}
func TestFleetTakeoverBeforeShippingStartsFromBase(t *testing.T) {
	f := fleetSetup(t)
	oldDir := filepath.Join(t.TempDir(), "old")
	bare := testenv.Git(t, f.dir, "remote", "get-url", "origin")
	testenv.Git(t, f.dir, "clone", bare, oldDir)
	testenv.WriteFile(t, filepath.Join(oldDir, "unpublished.txt"), "keep this work")
	now := time.Now()
	old := fleet.Store{Dir: oldDir, Now: func() time.Time { return now }}
	if _, err := old.Acquire(context.Background(), "add-login", "old", "run", "add-login", false, false); err != nil {
		t.Fatal(err)
	}
	f.env.SetPlan(t, map[string]testenv.Action{"build": tick("1.1", "1.2", "2.1", "2.2")})
	e := f.engine()
	e.Now = func() time.Time { return now.Add(time.Hour) }
	r, err := e.Run(context.Background(), Request{Change: "add-login", TakeOver: true})
	if err != nil || r.State != Ready || r.FleetClaim.Epoch != 2 || len(r.History) == 0 || r.History[0].Step != "build" {
		t.Fatalf("%+v %v", r, err)
	}
	data, err := os.ReadFile(filepath.Join(oldDir, "unpublished.txt"))
	if err != nil || string(data) != "keep this work" {
		t.Fatal("old worktree changed")
	}
}
func TestFleetMergedRemovesClaimAndFencesBranchDeletion(t *testing.T) {
	f, e, original := shippingSetup(t, config.ShipMerge, false, "add-login")
	f.cfg.Mode = config.ModeFleet
	bare := testenv.Git(t, f.dir, "remote", "get-url", "origin")
	api := &fleetShippingForge{shippingForge: original, bare: bare}
	e.Forge = func(context.Context) (forge.Client, func(string) string, error) { return api, nil, nil }
	r, err := e.Run(context.Background(), Request{Change: "add-login"})
	if err != nil || r.State != Merged {
		t.Fatalf("%+v %v", r, err)
	}
	all, err := f.fleetStore().Read(context.Background())
	if err != nil || len(all) != 0 {
		t.Fatalf("merged claim remains: %v %v", all, err)
	}
}

func TestFleetEngineReuseStillChecksNextScopeAdmission(t *testing.T) {
	f := fleetSetup(t)
	f.cfg.Fleet.MaxRuns = 1
	f.env.SetPlan(t, map[string]testenv.Action{"build": tick("1.1", "1.2")})
	e := f.engine()
	r, err := e.Run(context.Background(), Request{Change: "add-login", Scope: "1"})
	if err != nil || r.State != Ready {
		t.Fatalf("%+v %v", r, err)
	}
	store := f.fleetStore()
	if _, err := store.Acquire(context.Background(), "add-users", "two", "next", "add-users", false, false); err != nil {
		t.Fatal(err)
	}
	_, err = e.Run(context.Background(), Request{Change: "add-login", Scope: "2"})
	fleetErr(t, err, "fleet limit reached")
}
