package dispatch

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func machine(name string, capacity, runs int, agents ...Agent) Machine {
	if agents == nil {
		agents = []Agent{{Name: "claude", Installed: true, SignIn: SignedIn}}
	}
	return Machine{Name: name, Report: &Report{Name: name, FleetCapacity: capacity, FleetRuns: runs, Agents: agents, Checkout: true}}
}

func approved(id string, prefer ...string) Change {
	return Change{ID: id, Approved: true, Machines: prefer, Agents: []string{"claude"}}
}

func skipOf(t *testing.T, p Plan, id string) Skip {
	t.Helper()
	for _, s := range p.Skipped {
		if s.Change == id {
			return s
		}
	}
	t.Fatalf("%s not skipped: %+v", id, p)
	return Skip{}
}

func placed(p Plan) map[string]string {
	out := map[string]string{}
	for _, a := range p.Actions {
		out[a.Change] = string(a.Kind) + "@" + a.Machine
	}
	return out
}

// Scenario: Nothing to do.
func TestNothingToDo(t *testing.T) {
	p := Decide(Input{
		Changes:  []Change{approved("add-login"), {ID: "add-users", NotStartable: "waiting on add-login"}},
		Claims:   map[string]Claim{"add-login": {Machine: "mini", State: ClaimActive}},
		Machines: []Machine{machine("studio", 2, 0)},
	})
	if len(p.Actions) != 0 {
		t.Fatalf("started %+v", p.Actions)
	}
	if s := skipOf(t, p, "add-login"); s.Why != "claimed by mini (active)" {
		t.Errorf("add-login: %q", s.Why)
	}
	if s := skipOf(t, p, "add-users"); s.Why != "waiting on add-login" {
		t.Errorf("add-users: %q", s.Why)
	}
}

// Scenario: Unapproved change skipped.
func TestUnapprovedChangeSkipped(t *testing.T) {
	p := Decide(Input{
		Changes:  []Change{{ID: "add-login", NotStartable: "not approved"}},
		Machines: []Machine{machine("studio", 2, 0)},
	})
	if len(p.Actions) != 0 || skipOf(t, p, "add-login").Why != "not approved" {
		t.Fatalf("%+v", p)
	}
}

// Scenario: Agent signed out.
func TestAgentSignedOut(t *testing.T) {
	ch := approved("add-login")
	ch.Agents = []string{"claude", "codex"}
	p := Decide(Input{
		Changes: []Change{ch},
		Machines: []Machine{machine("mini", 2, 0,
			Agent{Name: "claude", Installed: true, SignIn: SignedIn},
			Agent{Name: "codex", Installed: true, SignIn: SignedOut})},
	})
	s := skipOf(t, p, "add-login")
	if len(p.Actions) != 0 || !reflect.DeepEqual(s.Notes, []string{"mini: codex signed out"}) {
		t.Fatalf("%+v", p)
	}
}

// Scenario: Sign-in unknown.
func TestSignInUnknown(t *testing.T) {
	ch := approved("add-login")
	ch.Agents = []string{"omp"}
	p := Decide(Input{
		Changes:  []Change{ch},
		Machines: []Machine{machine("lab", 1, 0, Agent{Name: "omp", Installed: true, SignIn: Unknown})},
	})
	if len(p.Actions) != 1 || p.Actions[0].Machine != "lab" {
		t.Fatalf("%+v", p)
	}
	if !reflect.DeepEqual(p.Actions[0].Notes, []string{"lab: omp sign-in unknown"}) {
		t.Errorf("notes %v", p.Actions[0].Notes)
	}
}

// Scenario: Preferred machine full; and the rest of the choice rule.
func TestPreferredMachineFull(t *testing.T) {
	ms := []Machine{machine("mini", 1, 1), machine("lab", 2, 0), machine("studio", 1, 0)}
	p := Decide(Input{Changes: []Change{approved("add-login", "mini")}, Machines: ms})
	if got := placed(p)["add-login"]; got != "start@lab" {
		t.Fatalf("placed on %s", got)
	}
	// Preferred order wins over free capacity; capacity is taken within
	// the pass; ties go by name.
	ms = []Machine{machine("studio", 1, 0), machine("lab", 2, 0), machine("beta", 2, 0)}
	p = Decide(Input{Changes: []Change{approved("a", "studio"), approved("b", "studio"), approved("c"), approved("d"), approved("e")}, Machines: ms})
	want := map[string]string{"a": "start@studio", "b": "start@beta", "c": "start@lab", "d": "start@beta", "e": "start@lab"}
	if got := placed(p); !reflect.DeepEqual(got, want) {
		t.Errorf("placed %v, want %v", got, want)
	}
}

// Scenario: Machine not in the fleet; and machines without a checkout or
// a report are not candidates.
func TestCandidates(t *testing.T) {
	noCheckout := machine("lab", 2, 0)
	noCheckout.Report.Checkout = false
	p := Decide(Input{
		Changes:  []Change{approved("add-login")},
		Machines: []Machine{machine("mini", 0, 0), noCheckout, {Name: "studio", Err: errors.New("timed out")}},
	})
	s := skipOf(t, p, "add-login")
	want := []string{"mini: not in the fleet (fleet_capacity 0)", "lab: no checkout of this repository", "studio: unreachable"}
	if len(p.Actions) != 0 || !reflect.DeepEqual(s.Notes, want) {
		t.Fatalf("%+v", p)
	}
	if !reflect.DeepEqual(p.Unreachable, []string{"studio: unreachable (timed out)"}) {
		t.Errorf("unreachable %v", p.Unreachable)
	}
}

// The fleet limit counts active, parked and expired claims, not ready ones.
func TestFleetLimit(t *testing.T) {
	p := Decide(Input{
		Changes: []Change{approved("a"), approved("b"), approved("c")},
		Claims: map[string]Claim{"x": {Machine: "mini", State: ClaimParked}, "y": {Machine: "mini", State: ClaimReady},
			"z": {Machine: "mini", State: ClaimExpired}},
		Machines: []Machine{machine("studio", 4, 0)},
		MaxRuns:  3,
	})
	if got := placed(p); !reflect.DeepEqual(got, map[string]string{"a": "start@studio"}) {
		t.Fatalf("placed %v", got)
	}
	if s := skipOf(t, p, "b"); !strings.Contains(s.Why, "fleet limit reached (max_runs 3)") {
		t.Errorf("b: %q", s.Why)
	}
}

var since = time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)

func holder(name string, capacity, fleetRuns int, runs ...RunReport) Machine {
	m := machine(name, capacity, fleetRuns)
	m.Report.Runs = runs
	return m
}

// Scenario: Next group across passes (the plan's half; the end-to-end
// half is in the CLI tests).
func TestNextGroupAcrossPasses(t *testing.T) {
	login := RunReport{Change: "add-login", State: "ready", Since: since, Scope: "1", Dispatched: true}
	in := Input{
		Changes: []Change{approved("add-login"), approved("add-users")},
		Claims: map[string]Claim{"add-login": {Machine: "studio", State: ClaimReady},
			"add-users": {Machine: "mini", State: ClaimActive}},
		Machines: []Machine{holder("studio", 1, 0, login), holder("mini", 1, 1)},
		MaxRuns:  1,
	}
	p := Decide(in)
	if len(p.Actions) != 0 || !strings.Contains(skipOf(t, p, "add-login").Why, "waiting for room") {
		t.Fatalf("first pass %+v", p)
	}
	in.Claims["add-users"] = Claim{Machine: "mini", State: ClaimReady}
	in.Machines[1].Report.Runs = []RunReport{{Change: "add-users", State: "ready", Since: since, Complete: true}}
	p = Decide(in)
	if len(p.Actions) != 1 {
		t.Fatalf("second pass %+v", p)
	}
	a := p.Actions[0]
	want := []string{"run", "--for", "github.com/sam/app", "--expect-state", "ready", "--expect-since", since.Format(time.RFC3339Nano), "--tasks", "next", "add-login"}
	if a.Machine != "studio" || !reflect.DeepEqual(a.Args("github.com/sam/app"), want) {
		t.Errorf("action %+v args %v", a, a.Args("github.com/sam/app"))
	}
}

// Scenario: Complete ready run; Stopped by a person.
func TestNotContinued(t *testing.T) {
	p := Decide(Input{
		Changes: []Change{approved("add-login"), approved("add-users"), approved("add-roles")},
		Claims: map[string]Claim{"add-login": {Machine: "studio", State: ClaimReady},
			"add-users": {Machine: "studio", State: ClaimParked}, "add-roles": {Machine: "studio", State: ClaimReady}},
		Machines: []Machine{holder("studio", 2, 0,
			RunReport{Change: "add-login", State: "ready", Since: since, Complete: true},
			RunReport{Change: "add-users", State: "stopped", Since: since, Scope: "1"},
			RunReport{Change: "add-roles", State: "ready", Since: since})},
		GroupScope: false,
	})
	if len(p.Actions) != 0 {
		t.Fatalf("continued %+v", p.Actions)
	}
	if s := skipOf(t, p, "add-login"); !strings.Contains(s.Why, "complete") {
		t.Errorf("add-login: %q", s.Why)
	}
	if s := skipOf(t, p, "add-users"); s.Why != "claimed by studio (parked)" {
		t.Errorf("add-users: %q", s.Why)
	}
	if s := skipOf(t, p, "add-roles"); !strings.Contains(s.Why, "by hand") {
		t.Errorf("add-roles: %q", s.Why)
	}
}

// Scenario: Continuations first.
func TestContinuationsFirst(t *testing.T) {
	p := Decide(Input{
		// add-users is first in the Approved column, yet the continuation
		// takes the room.
		Changes: []Change{approved("add-users"), approved("add-login"), approved("add-roles")},
		Claims: map[string]Claim{"add-login": {Machine: "studio", State: ClaimReady},
			"add-roles": {Machine: "mini", State: ClaimActive}},
		Machines:   []Machine{holder("studio", 2, 0, RunReport{Change: "add-login", State: "ready", Since: since, Scope: "1"})},
		MaxRuns:    2,
		GroupScope: true,
	})
	if got := placed(p); !reflect.DeepEqual(got, map[string]string{"add-login": "continue@studio"}) {
		t.Fatalf("placed %v", got)
	}
	if s := skipOf(t, p, "add-users"); !strings.Contains(s.Why, "fleet limit") {
		t.Errorf("add-users: %q", s.Why)
	}
}

// Scenario: Holder unreachable.
func TestHolderUnreachable(t *testing.T) {
	p := Decide(Input{
		Changes:    []Change{approved("add-login"), approved("add-users")},
		Claims:     map[string]Claim{"add-login": {Machine: "studio", State: ClaimReady}},
		Machines:   []Machine{{Name: "studio", Err: errors.New("timed out")}, machine("mini", 1, 0)},
		GroupScope: true,
	})
	if got := placed(p); !reflect.DeepEqual(got, map[string]string{"add-users": "start@mini"}) {
		t.Fatalf("placed %v", got)
	}
	if s := skipOf(t, p, "add-login"); !strings.Contains(s.Why, "studio, which is unreachable") {
		t.Errorf("add-login: %q", s.Why)
	}
	if len(p.Unreachable) != 1 || !strings.HasPrefix(p.Unreachable[0], "studio: unreachable") {
		t.Errorf("unreachable %v", p.Unreachable)
	}
}

// A continuation needs the holder's agents, a fleet slot there when the
// run was dispatched, and approval.
func TestContinuationNeeds(t *testing.T) {
	run := RunReport{Change: "add-login", State: "ready", Since: since, Scope: "1", Dispatched: true}
	base := func() Input {
		return Input{
			Changes:    []Change{approved("add-login")},
			Claims:     map[string]Claim{"add-login": {Machine: "studio", State: ClaimReady}},
			Machines:   []Machine{holder("studio", 1, 0, run)},
			GroupScope: true,
		}
	}
	in := base()
	in.Machines[0].Report.FleetRuns = 1
	if p := Decide(in); len(p.Actions) != 0 || !strings.Contains(skipOf(t, p, "add-login").Why, "no free fleet capacity") {
		t.Errorf("full holder: %+v", p)
	}
	in = base()
	in.Machines[0].Report.Agents[0].SignIn = SignedOut
	if p := Decide(in); len(p.Actions) != 0 || !strings.Contains(skipOf(t, p, "add-login").Why, "claude signed out") {
		t.Errorf("signed out: %+v", p)
	}
	in = base()
	in.Changes[0].Approved = false
	if p := Decide(in); len(p.Actions) != 0 || !strings.Contains(skipOf(t, p, "add-login").Why, "approval") {
		t.Errorf("unapproved: %+v", p)
	}
	// A person's own run (not dispatched) takes no fleet slot.
	in = base()
	in.Machines[0].Report.FleetCapacity = 0
	in.Machines[0].Report.Runs[0].Dispatched = false
	if p := Decide(in); len(p.Actions) != 1 {
		t.Errorf("own run: %+v", p)
	}
}

func TestStartArgs(t *testing.T) {
	a := Action{Kind: Start, Change: "add-login", Machine: "mini"}
	want := []string{"run", "--for", "github.com/sam/app", "--dispatched", "add-login"}
	if got := a.Args("github.com/sam/app"); !reflect.DeepEqual(got, want) {
		t.Errorf("args %v", got)
	}
}

// A change with a run on any machine is not placed again, even without a
// fleet claim: a run started in solo mode before the repository became a
// fleet keeps working unclaimed.
func TestExistingRunNotPlacedAgain(t *testing.T) {
	p := Decide(Input{
		Changes: []Change{approved("add-login", "mini"), approved("add-users")},
		Machines: []Machine{
			holder("studio", 0, 0, RunReport{Change: "add-login", State: "running", Since: since, Mode: "solo"}),
			holder("lab", 1, 0, RunReport{Change: "add-users", State: "stopped", Since: since, Mode: "fleet"}),
			machine("mini", 2, 0),
		},
	})
	if len(p.Actions) != 0 {
		t.Fatalf("placed %+v", p.Actions)
	}
	if s := skipOf(t, p, "add-login"); s.Why != "has a run on studio (running, started in solo mode) and no fleet claim; not placed again" {
		t.Errorf("add-login: %q", s.Why)
	}
	if s := skipOf(t, p, "add-users"); s.Why != "has a run on lab (stopped) and no fleet claim; not placed again" {
		t.Errorf("add-users: %q", s.Why)
	}
}

// A new start warns about each machine that didn't answer: it may still
// run the change unclaimed, in solo mode. Continuations, which go to the
// claim's holder, don't.
func TestUnreachableMachineWarning(t *testing.T) {
	p := Decide(Input{
		Changes: []Change{approved("add-login"), approved("add-users")},
		Claims:  map[string]Claim{"add-users": {Machine: "mini", State: ClaimReady}},
		Machines: []Machine{{Name: "studio", Err: errors.New("timed out")},
			holder("mini", 2, 0, RunReport{Change: "add-users", State: "ready", Since: since, Scope: "1"})},
		GroupScope: true,
	})
	if len(p.Actions) != 2 {
		t.Fatalf("%+v", p)
	}
	for _, a := range p.Actions {
		switch a.Kind {
		case Start:
			want := []string{"studio did not answer; if it still runs add-login in solo mode, this may start it twice"}
			if !reflect.DeepEqual(a.Warnings, want) {
				t.Errorf("start warnings %v", a.Warnings)
			}
		case Continue:
			if len(a.Warnings) != 0 {
				t.Errorf("continuation warnings %v", a.Warnings)
			}
		}
	}
}
