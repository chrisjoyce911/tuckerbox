package runs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// crash makes change's run look as if its process died while running:
// run.json says running with a new since, and nobody holds the lock.
func (f *fixture) crash(change string) *Run {
	f.t.Helper()
	r := f.current(change)
	r.State, r.Step, r.Since = Running, "build", time.Now().Add(-time.Second).Round(0)
	if err := WriteRun(f.state(change).RunFile(), r); err != nil {
		f.t.Fatal(err)
	}
	return f.current(change)
}

func changed(t *testing.T, err error) {
	t.Helper()
	var ce *ChangedError
	if !errors.As(err, &ce) {
		t.Fatalf("want a ChangedError, got %v", err)
	}
}

// A conditional request goes ahead only on the run it saw; a match runs,
// and history says autopilot asked.
func TestConditionalRequests(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	e := f.engine()
	ctx := context.Background()
	// No run, as seen: it starts, by autopilot.
	r, err := e.Run(ctx, Request{Change: "add-login", Expect: &Expect{NoRun: true}, By: "autopilot"})
	if err != nil || r.State != Ready {
		t.Fatalf("start: %+v %v", r, err)
	}
	events, _ := history.Read(history.File(f.state("add-login")))
	if events[0].Kind != history.Started || events[0].By != "autopilot" {
		t.Errorf("first event %+v", events[0])
	}
	// It has a run now: a start that saw none changes nothing.
	before := *f.current("add-login")
	_, err = f.engine().Run(ctx, Request{Change: "add-login", Expect: &Expect{NoRun: true}})
	changed(t, err)
	if now := f.current("add-login"); now.State != before.State || !now.Since.Equal(before.Since) || now.Round != before.Round {
		t.Errorf("run changed: %+v", now)
	}

	// Scenario: Crashed again after a person resumed: autopilot saw the
	// first crash; the run crashed again (a new since) before its resume
	// took the lock.
	first := f.crash("add-login")
	second := f.crash("add-login")
	if first.Since.Equal(second.Since) {
		t.Fatal("since did not move")
	}
	_, err = f.engine().Run(ctx, Request{Change: "add-login", Mode: Resume, Worktree: f.worktree("add-login"),
		Expect: &Expect{State: first.State, Since: first.Since}, By: "autopilot"})
	changed(t, err)

	// Scenario: Unapproved while crashed: the resume gate refuses under
	// the lock.
	e = f.engine()
	e.ResumeGate = func(context.Context, string, string) error { return errors.New("no longer approved") }
	_, err = e.Run(ctx, Request{Change: "add-login", Mode: Resume, Worktree: f.worktree("add-login"),
		Expect: &Expect{State: second.State, Since: second.Since}, By: "autopilot"})
	changed(t, err)
	if now := f.current("add-login"); now.State != Running || !now.Since.Equal(second.Since) {
		t.Errorf("refused resume changed the run: %+v", now)
	}

	// As seen: the crash is resumed, by autopilot, in a new round.
	round := second.Round
	r, err = f.engine().Run(ctx, Request{Change: "add-login", Mode: Resume, Worktree: f.worktree("add-login"),
		Expect: &Expect{State: second.State, Since: second.Since}, By: "autopilot"})
	if err != nil || r.State != Ready || r.Round != round+1 {
		t.Fatalf("resume: %+v %v", r, err)
	}
	events, _ = history.Read(history.File(f.state("add-login")))
	var resumed *history.Event
	for i := range events {
		if events[i].Kind == history.Resumed {
			resumed = &events[i]
		}
	}
	if resumed == nil || resumed.By != "autopilot" {
		t.Errorf("resumed event %+v", resumed)
	}
}

// Scenario: Stopped after the pass looked: a person resumed the crashed
// run and stopped it; autopilot's resume of the crash changes nothing and
// the run stays stopped.
func TestStoppedAfterThePassLooked(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	gate := f.gate("build")
	f.env.SetPlan(t, map[string]testenv.Action{"build:1": {Status: "blocked"}, "build": {WaitFor: gate}})
	f.run(Request{Change: "add-login"})
	seen := f.crash("add-login")
	// The person resumes and stops it.
	ctx, cancel := context.WithCancel(context.Background())
	ch := f.start(ctx, Request{Change: "add-login", Mode: Resume, Worktree: f.worktree("add-login")})
	f.waitState("add-login", Running, "build")
	cancel()
	if res := <-ch; res.err != nil || res.run.State != Stopped {
		t.Fatalf("person's resume: %+v %v", res.run, res.err)
	}
	_, err := f.engine().Run(context.Background(), Request{Change: "add-login", Mode: Resume, Worktree: f.worktree("add-login"),
		Expect: &Expect{State: seen.State, Since: seen.Since}, By: "autopilot"})
	changed(t, err)
	if r := f.current("add-login"); r.State != Stopped {
		t.Errorf("run is %s", r.State)
	}
	open_(t, gate)
}

// Scenario: Unapproved after the pass looked: the pass saw the change
// approved; the owner unapproved it before the start took the lock. The
// gate, judged under the lock, refuses and nothing is made.
func TestUnapprovedAfterThePassLooked(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	e := f.engine()
	e.Gate = func(context.Context, string, StartPoint) error { return errors.New("not approved") }
	if _, err := e.Run(context.Background(), Request{Change: "add-login", Expect: &Expect{NoRun: true}, By: "autopilot"}); err == nil {
		t.Fatal("started")
	}
	if f.current("add-login") != nil {
		t.Error("run state made")
	}
}

