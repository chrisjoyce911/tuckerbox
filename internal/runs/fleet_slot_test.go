package runs

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/fleetslot"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// Scenario: Two repositories, one place. With fleet_capacity = 1, a
// dispatched run of one repository holds the machine's fleet slot; a
// dispatched run of another repository refuses with "mini is full",
// makes no worktree and no run, until the first one ends.
func TestTwoRepositoriesOnePlace(t *testing.T) {
	env := tools.Setup(t)
	a := open(t, env, tools.NewRepo(t, testenv.RepoOptions{Changes: []string{"add-login"}}))
	b := open(t, env, tools.NewRepo(t, testenv.RepoOptions{Changes: []string{"add-users"}}))
	for _, f := range []*fixture{a, b} {
		f.cfg.Machine, f.cfg.FleetCapacity = "mini", 1
	}
	gate := a.gate("build")
	env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: gate}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := a.start(ctx, Request{Change: "add-login", Dispatched: true})
	a.waitState("add-login", Running, "build")
	if r := a.current("add-login"); !r.Dispatched {
		t.Fatal("run.json does not record dispatched")
	}

	_, err := b.engine().Run(context.Background(), Request{Change: "add-users", Dispatched: true})
	if !errors.Is(err, fleetslot.ErrFull) || err.Error() != "mini is full" {
		t.Fatalf("second dispatched run: %v, want mini is full", err)
	}
	if _, err := os.Stat(b.worktree("add-users")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the refused run made a worktree: %v", err)
	}

	open_(t, gate)
	if res := <-done; res.err != nil || res.run.State != Ready {
		t.Fatalf("first run: %+v %v", res.run, res.err)
	}
	// The slot is free once the first run has ended.
	if r := b.run(Request{Change: "add-users", Dispatched: true}); r.State != Ready || !r.Dispatched {
		t.Fatalf("second run after the first ended: %s %v", r.State, r.Dispatched)
	}
}

// Scenario: Person's own run. A run a person starts takes no fleet slot,
// even when every slot is held, and is not recorded as dispatched.
func TestPersonsOwnRun(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-users"}})
	f.cfg.Machine, f.cfg.FleetCapacity = "mini", 1
	held, err := fleetslot.Take(f.env.StateHome, "mini", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Free()
	r := f.run(Request{Change: "add-users"})
	if r.State != Ready || r.Dispatched {
		t.Fatalf("person's run: %s dispatched=%v", r.State, r.Dispatched)
	}
}

// A machine without fleet_capacity takes no dispatched run.
func TestDispatchedRunWithoutCapacity(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-users"}})
	f.cfg.Machine = "laptop"
	_, err := f.engine().Run(context.Background(), Request{Change: "add-users", Dispatched: true})
	if err == nil || !strings.Contains(err.Error(), "laptop is full") {
		t.Fatalf("err %v", err)
	}
}

// Resume of a run recorded as dispatched takes a fleet slot again.
func TestResumeDispatchedTakesSlot(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.cfg.Machine, f.cfg.FleetCapacity = "mini", 1
	f.env.SetPlan(t, map[string]testenv.Action{"build:1": {Status: "blocked", Summary: "which database?"}})
	if r := f.run(Request{Change: "add-login", Dispatched: true}); r.State != Blocked {
		t.Fatalf("state %s", r.State)
	}
	// Blocked: the slot was freed.
	held, err := fleetslot.Take(f.env.StateHome, "mini", 1)
	if err != nil {
		t.Fatalf("slot not freed on block: %v", err)
	}
	_, err = f.engine().Run(context.Background(), Request{Change: "add-login", Mode: Resume, Worktree: f.worktree("add-login")})
	if !errors.Is(err, fleetslot.ErrFull) {
		t.Fatalf("resume with the slot held: %v", err)
	}
	held.Free()
	if r := f.run(Request{Change: "add-login", Mode: Resume, Worktree: f.worktree("add-login")}); r.State != Ready {
		t.Fatalf("resume: %s %s", r.State, r.Reason)
	}
}

// Changing mode does not change a run already started: it keeps the mode
// it started in, and a new run takes the repository's current mode.
func TestModeChangeKeepsStartedMode(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login", "add-users"}})
	f.env.SetPlan(t, map[string]testenv.Action{"build:1": {Status: "blocked", Summary: "which database?"}})
	if r := f.run(Request{Change: "add-login"}); r.State != Blocked || r.StartedMode() != config.ModeSolo || r.Mode != config.ModeSolo {
		t.Fatalf("first run: %s mode %q", r.State, r.Mode)
	}
	cfg := *f.cfg
	cfg.Mode = config.ModeTeam
	f.cfg = &cfg
	e := f.engine()
	ctx := context.Background()
	r, err := e.Run(ctx, Request{Change: "add-login", Mode: Resume, Worktree: f.worktree("add-login")})
	if err != nil || r.State != Ready {
		t.Fatalf("resume: %+v %v", r, err)
	}
	if r.Mode != config.ModeSolo || e.Config.Mode != config.ModeSolo {
		t.Fatalf("resumed run mode %q, engine mode %q; want solo", r.Mode, e.Config.Mode)
	}
	if f.cfg.Mode != config.ModeTeam {
		t.Fatal("the freeze changed the caller's config")
	}
	saved, err := f.repo.SavedRun(ctx, f.cfg.Worktree, "add-login")
	if err != nil || saved == nil || EffectiveMode(f.cfg, saved) != config.ModeSolo {
		t.Fatalf("SavedRun %+v %v", saved, err)
	}
	if none, err := f.repo.SavedRun(ctx, f.cfg.Worktree, "add-users"); none != nil || err != nil {
		t.Fatalf("SavedRun of an unstarted change: %+v %v", none, err)
	}
	if EffectiveMode(f.cfg, nil) != config.ModeTeam {
		t.Fatal("a new start takes the current mode")
	}
	// A run from before modes records none and is solo.
	if (&Run{}).StartedMode() != config.ModeSolo {
		t.Fatal("old run")
	}
}

