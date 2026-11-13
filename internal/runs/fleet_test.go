package runs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/fleet"
	"github.com/chrisjoyce911/tuckerbox/internal/fleetslot"
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
