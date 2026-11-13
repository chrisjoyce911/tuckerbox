// Package dispatch is tuckerbox dispatch's placement: in one pass it
// decides which approved changes of a fleet repository to start, on which
// machines, and which ready runs to continue on the machine holding their
// claim (fleet-placement).
//
// A plan is advice. It reserves nothing: the run each action starts takes
// its machine's fleet slot and claims the change itself, and refuses when
// another dispatcher got there first. Plan is pure; the CLI gathers its
// input (the fetched base, the fleet ref, each machine's report) and runs
// the actions.
package dispatch

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Sign-in states a machine reports for an agent.
const (
	SignedIn  = "signed in"
	SignedOut = "signed out"
	Unknown   = "unknown"
)

// Claim states, as a reader of the fleet ref works them out (expired is
// not stored: an active claim not renewed within the lease).
const (
	ClaimActive  = "active"
	ClaimParked  = "parked"
	ClaimReady   = "ready"
	ClaimExpired = "expired"
)

// Agent is one agent CLI in a machine's report.
type Agent struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	// SignIn is SignedIn, SignedOut or Unknown.
	SignIn string `json:"sign_in"`
}

// RunReport is one run of the repository in a machine's report.
type RunReport struct {
	Change     string    `json:"change"`
	State      string    `json:"state"`
	Since      time.Time `json:"since"`
	Scope      string    `json:"scope,omitempty"`
	Complete   bool      `json:"complete"`
	Dispatched bool      `json:"dispatched"`
}

// Report is what tuckerbox machine --json --for <remote> says about a
// machine; only the fields dispatch reads.
type Report struct {
	Name          string      `json:"name"`
	FleetCapacity int         `json:"fleet_capacity"`
	FleetRuns     int         `json:"fleet_runs"`
	Agents        []Agent     `json:"agents"`
	Checkout      bool        `json:"checkout"`
	Path          string      `json:"path,omitempty"`
	Runs          []RunReport `json:"runs"`
}

// Free is the report's free fleet capacity.
func (r Report) Free() int {
	if f := r.FleetCapacity - r.FleetRuns; f > 0 {
		return f
	}
	return 0
}

func (r Report) agent(name string) (Agent, bool) {
	for _, a := range r.Agents {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}

func (r Report) run(change string) (RunReport, bool) {
	for _, x := range r.Runs {
		if x.Change == change {
			return x, true
		}
	}
	return RunReport{}, false
}

// Machine is one machine dispatch may place work on, with its report or
// why it gave none.
type Machine struct {
	Name string
	// Local is this machine.
	Local  bool
	Report *Report
	// Err is why the machine gave no report (unreachable, a bad answer).
	Err error
}

// Claim is a change's claim in the fleet ref.
type Claim struct {
	Machine string
	// State is ClaimActive, ClaimParked, ClaimReady or ClaimExpired.
	State string
}

// counts reports whether the claim fills a place under max_runs: every
// claim but a ready one (an expired claim is still an active one in the
// ref).
func (c Claim) counts() bool { return c.State != ClaimReady }

// Change is one active change at the fetched base, in admission order.
type Change struct {
	ID string
	// NotStartable is why the change may not start now ("" when it may):
	// invalid, not approved, overrides not allowed, dependencies not met.
	NotStartable string
	// Approved is whether the run gate passes, for a continuation.
	Approved bool
	// Machines is the change's preferred machines, in order.
	Machines []string
	// Agents lists the effective agent of each of the change's steps.
	Agents []string
}

// Input is everything a pass decides on.
type Input struct {
	// Changes are the active changes in the Approved column's order.
	Changes []Change
	// Claims are the fleet ref's claims, by change.
	Claims map[string]Claim
	// Machines are the candidate machines: this one and each
	// [machines.*] entry.
	Machines []Machine
	// MaxRuns is [fleet] max_runs; 0 is no limit.
	MaxRuns int
	// GroupScope is [autopilot] scope = "group": a ready run without a
	// scope is continued with --tasks next.
	GroupScope bool
}

// Kind is what an action does.
type Kind string

const (
	// Start places a new change on a machine.
	Start Kind = "start"
	// Continue runs a ready run's next group on the machine holding its
	// claim.
	Continue Kind = "continue"
)

// Action is a run dispatch starts.
type Action struct {
	Kind    Kind   `json:"kind"`
	Change  string `json:"change"`
	Machine string `json:"machine"`
	// ExpectState and ExpectSince, for Continue, are the run as the
	// holder's report said.
	ExpectState string     `json:"expect_state,omitempty"`
	ExpectSince *time.Time `json:"expect_since,omitempty"`
	// Notes are the candidate reasons, for --dry-run and the output.
	Notes []string `json:"notes,omitempty"`
}

// Args is the tuckerbox command the action runs on its machine.
func (a Action) Args(remote string) []string {
	if a.Kind == Continue {
		return []string{"run", "--for", remote, "--expect-state", a.ExpectState,
			"--expect-since", a.ExpectSince.Format(time.RFC3339Nano), "--tasks", "next", a.Change}
	}
	return []string{"run", "--for", remote, "--dispatched", a.Change}
}

// Skip is a change the pass does not start, and why.
type Skip struct {
	Change string `json:"change"`
	Why    string `json:"why"`
	// Notes are each machine's reasons when no machine fits.
	Notes []string `json:"notes,omitempty"`
}

// Plan is what a pass would do.
type Plan struct {
	Actions []Action `json:"actions"`
	Skipped []Skip   `json:"skipped"`
	// Unreachable names the machines that gave no report, with why.
	Unreachable []string `json:"unreachable"`
}

// Decide makes a pass's plan: continuations first, then new changes, each
// in admission order, while the fleet limit has room.
func Decide(in Input) Plan {
	p := Plan{Actions: []Action{}, Skipped: []Skip{}, Unreachable: []string{}}
	free := map[string]int{}
	byName := map[string]Machine{}
	for _, m := range in.Machines {
		byName[m.Name] = m
		if m.Report != nil {
			free[m.Name] = m.Report.Free()
		} else {
			p.Unreachable = append(p.Unreachable, fmt.Sprintf("%s: unreachable (%v)", m.Name, m.Err))
		}
	}
	used := 0
	for _, c := range in.Claims {
		if c.counts() {
			used++
		}
	}
	room := func() bool { return in.MaxRuns <= 0 || used < in.MaxRuns }
	full := func() string {
		return fmt.Sprintf("waiting for room: fleet limit reached (max_runs %d)", in.MaxRuns)
	}
	skip := func(id, why string, notes ...string) {
		p.Skipped = append(p.Skipped, Skip{Change: id, Why: why, Notes: notes})
	}

	// Continuations: approved changes whose claim is ready and whose run
	// on the holder isn't complete.
	for _, ch := range in.Changes {
		cl, ok := in.Claims[ch.ID]
		if !ok || cl.State != ClaimReady {
			continue
		}
		m, known := byName[cl.Machine]
		switch {
		case !known:
			skip(ch.ID, fmt.Sprintf("ready on %s, which is not in this machine's [machines]", cl.Machine))
			continue
		case m.Report == nil:
			skip(ch.ID, fmt.Sprintf("ready on %s, which is unreachable; not continued elsewhere", cl.Machine))
			continue
		}
		r, ok := m.Report.run(ch.ID)
		switch {
		case !ok:
			skip(ch.ID, fmt.Sprintf("claimed ready by %s, which reports no run of it", cl.Machine))
			continue
		case r.State != ClaimReady:
			skip(ch.ID, fmt.Sprintf("claimed by %s, its run is %s", cl.Machine, r.State))
			continue
		case r.Complete:
			skip(ch.ID, fmt.Sprintf("ready on %s and complete; waiting to ship", cl.Machine))
			continue
		case r.Scope == "" && !in.GroupScope:
			skip(ch.ID, fmt.Sprintf("ready on %s but not complete; [autopilot] scope is change, so continue it by hand", cl.Machine))
			continue
		case !ch.Approved:
			skip(ch.ID, fmt.Sprintf("ready on %s; needs approval again before its next group", cl.Machine))
			continue
		case !room():
			skip(ch.ID, fmt.Sprintf("ready on %s, %s", cl.Machine, full()))
			continue
		}
		if why, _ := agentsOK(*m.Report, ch.Agents); why != "" {
			skip(ch.ID, fmt.Sprintf("ready on %s, but %s", cl.Machine, why))
			continue
		}
		if r.Dispatched && free[m.Name] <= 0 {
			skip(ch.ID, fmt.Sprintf("ready on %s, which has no free fleet capacity", cl.Machine))
			continue
		}
		if r.Dispatched {
			free[m.Name]--
		}
		since := r.Since
		used++
		p.Actions = append(p.Actions, Action{Kind: Continue, Change: ch.ID, Machine: m.Name,
			ExpectState: r.State, ExpectSince: &since})
	}

	// New changes: startable and unclaimed.
	for _, ch := range in.Changes {
		if cl, ok := in.Claims[ch.ID]; ok {
			if cl.State != ClaimReady {
				skip(ch.ID, fmt.Sprintf("claimed by %s (%s)", cl.Machine, cl.State))
			}
			continue
		}
		if ch.NotStartable != "" {
			skip(ch.ID, ch.NotStartable)
			continue
		}
		if !room() {
			skip(ch.ID, full())
			continue
		}
		var cands []string
		var notes []string
		for _, m := range in.Machines {
			ok, note := candidate(m, free[m.Name], ch.Agents)
			if note != "" {
				notes = append(notes, m.Name+": "+note)
			}
			if ok {
				cands = append(cands, m.Name)
			}
		}
		if len(cands) == 0 {
			skip(ch.ID, "no machine fits", notes...)
			continue
		}
		pick := choose(cands, ch.Machines, free)
		free[pick]--
		used++
		p.Actions = append(p.Actions, Action{Kind: Start, Change: ch.ID, Machine: pick, Notes: notes})
	}
	return p
}

// candidate reports whether m may take a new change that uses agents,
// with free places left in this pass, and a note on why not, or on an
// unknown sign-in.
func candidate(m Machine, free int, agents []string) (bool, string) {
	switch {
	case m.Report == nil:
		return false, "unreachable"
	case free <= 0:
		if m.Report.FleetCapacity == 0 {
			return false, "not in the fleet (fleet_capacity 0)"
		}
		return false, "no free fleet capacity"
	case !m.Report.Checkout:
		return false, "no checkout of this repository"
	}
	why, note := agentsOK(*m.Report, agents)
	if why != "" {
		return false, why
	}
	return true, note
}

// agentsOK returns why r can't run agents (an agent not installed or
// signed out), or "" with a note naming agents whose sign-in is unknown.
func agentsOK(r Report, agents []string) (why, note string) {
	var unknown []string
	seen := map[string]bool{}
	for _, name := range agents {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		a, ok := r.agent(name)
		switch {
		case !ok || !a.Installed:
			return name + " not installed", ""
		case a.SignIn == SignedOut:
			return name + " signed out", ""
		case a.SignIn != SignedIn:
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		return "", strings.Join(unknown, ", ") + " sign-in unknown"
	}
	return "", ""
}

// choose picks among candidates: the first of the change's preferred
// machines that is one, else the most free capacity, else the name.
func choose(cands, preferred []string, free map[string]int) string {
	is := map[string]bool{}
	for _, c := range cands {
		is[c] = true
	}
	for _, p := range preferred {
		if is[p] {
			return p
		}
	}
	sorted := append([]string(nil), cands...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if free[sorted[i]] != free[sorted[j]] {
			return free[sorted[i]] > free[sorted[j]]
		}
		return sorted[i] < sorted[j]
	})
	return sorted[0]
}
