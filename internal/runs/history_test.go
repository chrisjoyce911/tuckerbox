package runs

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

func (f *fixture) history(change string) []history.Event {
	f.t.Helper()
	evs, err := history.Read(history.File(f.state(change)))
	if err != nil {
		f.t.Fatal(err)
	}
	return evs
}

// brief is "kind step round" for each event, with "->to" for a hand-off
// or a looping rejection.
func brief(evs []history.Event) []string {
	var out []string
	for _, e := range evs {
		s := fmt.Sprintf("%s %s %d", e.Kind, e.Step, e.Round)
		if e.To != "" {
			s += " ->" + e.To
		}
		out = append(out, s)
	}
	return out
}

func wantEvents(t *testing.T, evs []history.Event, want ...string) {
	t.Helper()
	if got := brief(evs); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("history:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A run that goes through without trouble records every state change,
// each with a time offset, the agent and the machine.
func TestHistoryOfAPlainRun(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Summary: "built it"}})
	if r := f.run(Request{Change: "fix-login"}); r.State != Ready {
		t.Fatalf("state %s", r.State)
	}
	evs := f.history("fix-login")
	wantEvents(t, evs,
		"started build 1", "queued build 1",
		"step_started build 1", "step_finished build 1", "handoff build 1 ->review",
		"step_started review 1", "step_finished review 1",
		"ready  1")
	machine := history.Machine()
	for _, e := range evs {
		if e.V != history.Version || e.Change != "fix-login" || e.Time.IsZero() || e.Machine != machine {
			t.Errorf("event %+v", e)
		}
		if e.Step != "" && e.Agent == "" {
			t.Errorf("event %s %s has no agent", e.Kind, e.Step)
		}
	}
	if evs[4].Summary != "built it" || evs[3].Status != "done" || evs[3].ExitCode == nil || *evs[3].ExitCode != 0 {
		t.Errorf("hand-off %+v, finished %+v", evs[4], evs[3])
	}
}

// Scenario: Rejection is recorded. The review step rejects round 1 with
// two findings: the history has a rejection for round 1 with both
// findings, followed by step-started for build in round 2.
func TestHistoryRejectionIsRecorded(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"review:1": {
		Status: "rejected", Summary: "not yet",
		Findings: []testenv.Finding{
			{Severity: "high", File: "login.go", Detail: "password is logged"},
			{Severity: "low", File: "login_test.go", Detail: "no test for lockout"},
		},
	}})
	if r := f.run(Request{Change: "fix-login"}); r.State != Ready {
		t.Fatalf("state %s: %s", r.State, r.Reason)
	}
	evs := f.history("fix-login")
	i := -1
	for j, e := range evs {
		if e.Kind == history.Rejected {
			i = j
		}
	}
	if i < 0 {
		t.Fatalf("no rejection in %v", brief(evs))
	}
	rej := evs[i]
	if rej.Step != "review" || rej.Round != 1 || rej.To != "build" || rej.Summary != "not yet" || len(rej.Findings) != 2 ||
		rej.Findings[0].Detail != "password is logged" || rej.Findings[1].Detail != "no test for lockout" {
		t.Errorf("rejection %+v", rej)
	}
	if next := evs[i+1]; next.Kind != history.StepStarted || next.Step != "build" || next.Round != 2 {
		t.Errorf("after the rejection: %s %s %d", next.Kind, next.Step, next.Round)
	}
}

// A rejection past max_rounds has no step to go back to, and is followed
// by blocked.
func TestHistoryRejectionThatBlocks(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"review": {Status: "rejected", Summary: "still wrong"}})
	if r := f.run(Request{Change: "fix-login"}); r.State != Blocked {
		t.Fatalf("state %s", r.State)
	}
	evs := f.history("fix-login")
	n := len(evs)
	if evs[n-2].Kind != history.Rejected || evs[n-2].To != "" || evs[n-1].Kind != history.Blocked ||
		!strings.Contains(evs[n-1].Reason, "review limit reached") {
		t.Errorf("end of history: %v %+v", brief(evs[n-2:]), evs[n-1])
	}
}

// Scenario: History survives a resume. A run stopped and later resumed
// keeps every earlier event, followed by stopped and resumed.
func TestHistorySurvivesAResume(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	gate := f.gate("review")
	f.env.SetPlan(t, map[string]testenv.Action{"review": {WaitFor: gate}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := f.start(ctx, Request{Change: "fix-login"})
	f.waitState("fix-login", Running, "review")
	cancel()
	if res := <-done; res.err != nil || res.run.State != Stopped {
		t.Fatalf("after stop: %+v %v", res.run, res.err)
	}
	before := brief(f.history("fix-login"))
	open_(t, gate)
	if r := f.run(Request{Change: "fix-login", Mode: Resume}); r.State != Ready {
		t.Fatalf("after resume: %s %s", r.State, r.Reason)
	}
	after := brief(f.history("fix-login"))
	if strings.Join(after[:len(before)], "\n") != strings.Join(before, "\n") {
		t.Fatalf("earlier events changed:\n%s\nwas:\n%s", strings.Join(after, "\n"), strings.Join(before, "\n"))
	}
	wantEvents(t, f.history("fix-login"),
		"started build 1", "queued build 1",
		"step_started build 1", "step_finished build 1", "handoff build 1 ->review",
		"step_started review 1",
		"stopped review 1",
		"resumed review 2", "queued review 2",
		"step_started review 2", "step_finished review 2",
		"ready  2")
}

// A run whose process died left no final state; the next resume records
// the stop it never wrote before resumed. Rerun records rerun.
func TestHistoryAfterACrashAndRerun(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"build:1": {Status: "blocked", Summary: "which database?"}})
	if r := f.run(Request{Change: "fix-login"}); r.State != Blocked {
		t.Fatalf("state %s", r.State)
	}
	// Pretend the next process died while running build.
	st := f.state("fix-login")
	r, err := ReadRun(st.RunFile())
	if err != nil {
		t.Fatal(err)
	}
	r.State = Running
	if err := WriteRun(st.RunFile(), r); err != nil {
		t.Fatal(err)
	}
	if r := f.run(Request{Change: "fix-login", Mode: Resume}); r.State != Ready {
		t.Fatalf("resume: %s %s", r.State, r.Reason)
	}
	if r := f.run(Request{Change: "fix-login", Mode: Rerun, Step: "review"}); r.State != Ready {
		t.Fatalf("rerun: %s %s", r.State, r.Reason)
	}
	evs := f.history("fix-login")
	got := strings.Join(brief(evs), "\n")
	for _, want := range []string{
		"blocked build 1\nstopped build 1\nresumed build 2\nqueued build 2",
		"ready  2\nrerun review 3\nqueued review 3\nstep_started review 3",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("history lacks\n%s\nin:\n%s", want, got)
		}
	}
	for _, e := range evs {
		if e.Kind == history.Stopped && e.Reason != Crashed {
			t.Errorf("stopped reason %q", e.Reason)
		}
	}
}

// A terminal agent that reports through its turn hook is ended by
// Tuckerbox, so its exit status is unknown (-1): step_finished leaves it
// out instead of showing "done (exit -1)". A known status is kept.
func TestResultEventLeavesOutAnUnknownExit(t *testing.T) {
	ws := Workspace{State: statedir.Dir(t.TempDir())}
	e := &Engine{Config: &config.Config{}}
	rs := &Run{Change: "fix-login", Step: "build", Round: 1}
	e.resultEvent(ws, rs, &agent.Result{Status: "done", Summary: "built it"}, -1)
	e.resultEvent(ws, rs, &agent.Result{Status: "failed", Summary: "broke"}, 2)
	evs, err := history.Read(history.File(ws.State))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].ExitCode != nil || evs[1].ExitCode == nil || *evs[1].ExitCode != 2 {
		t.Fatalf("events %+v", evs)
	}
}
