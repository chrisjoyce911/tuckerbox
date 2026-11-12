package runs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/scope"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// scopedTasks is the tasks.md of the scope tests: two groups and a ship
// task.
const scopedTasks = "# Tasks\n\n" +
	"## 1. First\n\n" +
	"- [ ] 1.1 One\n" +
	"- [ ] 1.2 Two\n\n" +
	"## 2. Second\n\n" +
	"- [ ] 2.1 Three\n" +
	"- [ ] 2.2 Four\n" +
	"- [ ] 2.3 (ship) Archive this change\n"

// withTasks commits tasks.md for each change on main.
func (f *fixture) withTasks(text string, changes ...string) {
	f.t.Helper()
	for _, c := range changes {
		testenv.WriteFile(f.t, TasksFile(f.dir, c), text)
	}
	testenv.Git(f.t, f.dir, "add", "-A")
	testenv.Git(f.t, f.dir, "commit", "-q", "-m", "tasks")
}

func (f *fixture) runErr(req Request) (*Run, error) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return f.engine().Run(ctx, req)
}

func scopeOf(r *Run) string {
	if r.Scope == nil {
		return "<none>"
	}
	return r.Scope.Spec + " " + strings.Join(r.Scope.Tasks, ",")
}

// Each step ticks the group in scope (the builder) and passes (the
// reviewer); group 2 is ticked only when the plan says so.
func tick(ns ...string) testenv.Action { return testenv.Action{Tick: ns} }

func TestShipDecision(t *testing.T) {
	cases := []struct {
		mode             config.ShipMode
		scoped, complete bool
		want             shipOutcome
	}{
		{"", false, false, outcomeReady},
		{config.ShipLocal, false, false, outcomeReady},
		{config.ShipLocal, true, false, outcomeReady},
		{config.ShipLocal, true, true, outcomeReady},
		{config.ShipLocal, false, true, outcomeReady},
		// Scenario: First group in merge mode: ready, nothing shipped.
		{"merge", true, false, outcomeReady},
		{"mr", true, false, outcomeReady},
		// Scenario: Last group in mr mode: the run ships.
		{"mr", true, true, outcomeShip},
		{"merge", true, true, outcomeShip},
		{"mr", false, true, outcomeShip},
		{"merge", false, true, outcomeShip},
		// Scenario: Whole-change run leaves a task: blocked.
		{"merge", false, false, outcomeBlocked},
		{"mr", false, false, outcomeBlocked},
	}
	for _, c := range cases {
		if got := shipDecision(c.mode, c.scoped, c.complete); got != c.want {
			t.Errorf("shipDecision(%q, scoped %v, complete %v) = %d, want %d", c.mode, c.scoped, c.complete, got, c.want)
		}
	}
}

// Scenario: Scope ticked. Scenario: Earlier group ready: ready and not
// complete. Scenario: Next group after a ready one: round keeps counting,
// a late result from the old round is ignored, same worktree. Scenario:
// Last group ready, with a ship task left unticked (Final group with an
// archive task): ready and complete in one write.
func TestScopedRunsGroupByGroup(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.withTasks(scopedTasks, "add-login")
	// Round 1 is rejected twice, so scope 1 is ready after round 3.
	f.env.SetPlan(t, map[string]testenv.Action{
		"build:1":  tick("1.1", "1.2"),
		"review:1": {Status: "rejected", Summary: "again"},
		"review:2": {Status: "rejected", Summary: "again"},
		"build:4":  {Tick: []string{"2.1", "2.2"}, StaleFirst: true},
	})
	r := f.run(Request{Change: "add-login", Scope: "1"})
	if r.State != Ready || r.Round != 3 || r.Complete || scopeOf(r) != "1 1.1,1.2" {
		t.Fatalf("scope 1: %s round %d complete %v scope %s reason %q", r.State, r.Round, r.Complete, scopeOf(r), r.Reason)
	}
	// The same scope again is refused as for any ready run (and here it
	// is all ticked, too).
	if _, err := f.runErr(Request{Change: "add-login", Scope: "1"}); err == nil {
		t.Error("scope 1 again started")
	}
	wt := f.worktree("add-login")
	r = f.run(Request{Change: "add-login", Scope: "2"})
	if r.State != Ready || r.Round != 4 || !r.Complete || scopeOf(r) != "2 2.1,2.2" || r.Worktree != wt {
		t.Fatalf("scope 2: %s round %d complete %v scope %s reason %q", r.State, r.Round, r.Complete, scopeOf(r), r.Reason)
	}
	// Rejections started again for scope 2, and the stale round-3 result
	// written first in round 4 was not taken.
	if r.Rejections != nil {
		t.Errorf("rejections %v", r.Rejections)
	}
	last := r.History[len(r.History)-2]
	if last.Step != "build" || last.Round != 4 || last.Summary != "fake build done" {
		t.Errorf("build entry %+v", last)
	}
	wantStarts(t, f.env,
		"add-login build 1", "add-login review 1", "add-login build 2", "add-login review 2",
		"add-login build 3", "add-login review 3", "add-login build 4", "add-login review 4")
	// The ready write carried complete: the file says so.
	saved := f.current("add-login")
	if saved.State != Ready || !saved.Complete {
		t.Errorf("run.json %s complete %v", saved.State, saved.Complete)
	}
	data := readFile(t, f.state("add-login").RunFile())
	var raw map[string]any
	if err := json.Unmarshal([]byte(data), &raw); err != nil || raw["complete"] != true {
		t.Errorf("run.json: %v %s", err, data)
	}
}

// Scenario: Task left unticked: blocked naming it; resume re-runs the
// last step and keeps the scope.
func TestScopeTaskLeftUnticked(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.withTasks(scopedTasks, "add-login")
	f.env.SetPlan(t, map[string]testenv.Action{"build": tick("2.1")})
	r := f.run(Request{Change: "add-login", Scope: "2"})
	if r.State != Blocked || r.Reason != "scope tasks not ticked: 2.2" || r.Step != "review" || r.Complete {
		t.Fatalf("%s %s %q", r.State, r.Step, r.Reason)
	}
	// A task the scope names that tasks.md no longer has is named too.
	p := TasksFile(f.worktree("add-login"), "add-login")
	testenv.WriteFile(t, p, strings.Replace(readFile(t, p), "- [ ] 2.2 Four\n", "", 1))
	r = f.run(Request{Change: "add-login", Mode: Resume})
	if r.State != Blocked || r.Reason != "scope tasks no longer in tasks.md: 2.2" || scopeOf(r) != "2 2.1,2.2" {
		t.Fatalf("%s %q scope %s", r.State, r.Reason, scopeOf(r))
	}
	wantStarts(t, f.env, "add-login build 1", "add-login review 1", "add-login review 2")
}

// Scenario: Resume keeps the scope. Rerun keeps it too. Scenario: Second
// scope while running: refused, pointing to the active run.
func TestResumeKeepsTheScope(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.withTasks(scopedTasks, "add-login")
	gate := f.gate("review")
	f.env.SetPlan(t, map[string]testenv.Action{
		"build":    tick("2.1", "2.2"),
		"review:1": {WaitFor: gate},
	})
	ctx, cancel := context.WithCancel(context.Background())
	ch := f.start(ctx, Request{Change: "add-login", Scope: "2"})
	f.waitState("add-login", Running, "review")
	_, err := f.runErr(Request{Change: "add-login", Scope: "1"})
	var ae *ActiveError
	if !errors.As(err, &ae) || !strings.Contains(err.Error(), "already has an active run") {
		t.Errorf("second scope while running: %v", err)
	}
	cancel()
	if res := <-ch; res.err != nil || res.run.State != Stopped || res.run.Step != "review" {
		t.Fatalf("stop: %+v %v", res.run, res.err)
	}
	open_(t, gate)
	r := f.run(Request{Change: "add-login", Mode: Resume})
	if r.State != Ready || scopeOf(r) != "2 2.1,2.2" {
		t.Fatalf("resume: %s scope %s %q", r.State, scopeOf(r), r.Reason)
	}
	r = f.run(Request{Change: "add-login", Mode: Rerun, Step: "build"})
	if r.State != Ready || scopeOf(r) != "2 2.1,2.2" {
		t.Fatalf("rerun: %s scope %s", r.State, scopeOf(r))
	}
	wantStarts(t, f.env, "add-login build 1", "add-login review 1", "add-login review 2",
		"add-login build 3", "add-login review 3")
}

// Scenario: Rest of the change: run without a scope after a scoped ready
// run starts again from the first step with no scope. A stopped run with a
// scope, started again with the same scope, continues where it was.
func TestRestOfTheChange(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.withTasks(scopedTasks, "add-login")
	f.env.SetPlan(t, map[string]testenv.Action{"build:1": tick("1.1", "1.2"), "build:2": tick("2.1", "2.2")})
	if r := f.run(Request{Change: "add-login", Scope: "1"}); r.State != Ready {
		t.Fatalf("scope 1: %s %q", r.State, r.Reason)
	}
	r := f.run(Request{Change: "add-login"})
	if r.State != Ready || r.Scope != nil || r.Round != 2 || !r.Complete {
		t.Fatalf("whole change: %s scope %s round %d complete %v", r.State, scopeOf(r), r.Round, r.Complete)
	}
	wantStarts(t, f.env, "add-login build 1", "add-login review 1", "add-login build 2", "add-login review 2")
}

func TestSameScopeContinues(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.withTasks(scopedTasks, "add-login")
	f.env.SetPlan(t, map[string]testenv.Action{"build": tick("1.1", "1.2"), "review:1": {Status: "blocked"}})
	if r := f.run(Request{Change: "add-login", Scope: "1"}); r.State != Blocked || r.Step != "review" {
		t.Fatalf("%s %s", r.State, r.Step)
	}
	// Scope 1 is all ticked now, but it is the run's own scope: run
	// continues from review, as without scopes.
	r := f.run(Request{Change: "add-login", Scope: "1"})
	if r.State != Ready || scopeOf(r) != "1 1.1,1.2" || r.Complete {
		t.Fatalf("%s scope %s %q", r.State, scopeOf(r), r.Reason)
	}
	wantStarts(t, f.env, "add-login build 1", "add-login review 1", "add-login review 2")
	// A scope tasks.md doesn't have is refused under the lock, and the
	// run stays ready.
	if _, err := f.runErr(Request{Change: "add-login", Scope: "7"}); err == nil || !strings.Contains(err.Error(), "tasks.md has no group 7") {
		t.Errorf("group 7: %v", err)
	}
	if r := f.current("add-login"); r.State != Ready || scopeOf(r) != "1 1.1,1.2" {
		t.Errorf("after refusal: %s %s", r.State, scopeOf(r))
	}
}

// Scenario: Final group with an archive task: 2.3 is a ship task, so
// ticking 2.1 and 2.2 completes the change. Scenario: Whole-change run in
// local mode is ready whatever its ticks, recorded as not complete.
func TestCompleteIgnoresShipTasks(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"a", "b"}, MaxParallel: 2})
	f.withTasks(strings.NewReplacer("- [ ] 1.", "- [x] 1.").Replace(scopedTasks), "a", "b")
	f.env.SetPlan(t, map[string]testenv.Action{"a:build": tick("2.1", "2.2"), "b:build": tick("2.1")})
	r := f.run(Request{Change: "a", Scope: "next"})
	if r.State != Ready || !r.Complete || scopeOf(r) != "2 2.1,2.2" {
		t.Fatalf("a: %s complete %v scope %s %q", r.State, r.Complete, scopeOf(r), r.Reason)
	}
	r = f.run(Request{Change: "b"})
	if r.State != Ready || r.Complete || r.Scope != nil {
		t.Fatalf("b: %s complete %v %q", r.State, r.Complete, r.Reason)
	}
}

// Scenario: First group in merge mode: the run is ready, recorded as not
// complete, and nothing is shipped (the engine's half; the forge half is
// add-forge-shipping's). Scenario: Whole-change run leaves a task:
// blocked with the unticked tasks, nothing shipped.
func TestPartialRunsDontShip(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"a", "b"}, MaxParallel: 2})
	f.withTasks(scopedTasks, "a", "b")
	f.cfg.Ship = "merge"
	f.env.SetPlan(t, map[string]testenv.Action{"a:build": tick("1.1", "1.2"), "b:build": tick("1.1", "1.2", "2.1")})
	r := f.run(Request{Change: "a", Scope: "1"})
	if r.State != Ready || r.Complete || r.Reason != "" {
		t.Fatalf("a: %s complete %v %q", r.State, r.Complete, r.Reason)
	}
	r = f.run(Request{Change: "b"})
	if r.State != Blocked || r.Reason != "tasks not ticked: 2.2" {
		t.Fatalf("b: %s %q", r.State, r.Reason)
	}
	if n := testenv.Git(t, f.dir, "rev-list", "--count", "main..b"); n != "0" {
		t.Errorf("blocked run committed %s commits", n)
	}
	for _, root := range []string{f.dir, f.worktree("a"), f.worktree("b")} {
		if _, err := os.Stat(filepath.Join(root, "openspec", "changes", "archive")); err == nil {
			t.Errorf("%s: something was archived", root)
		}
	}
}

// fakeRun writes change's run.json in its worktree, making the worktree.
func (f *fixture) fakeRun(change string, r Run) {
	f.t.Helper()
	ws, err := f.repo.EnsureWorktree(context.Background(), f.cfg.Worktree, change, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	r.Change, r.Worktree, r.Branch = change, ws.Path, ws.Branch
	if err := WriteRun(ws.State.RunFile(), &r); err != nil {
		f.t.Fatal(err)
	}
}

// Scenario: Dependency ready for one group: B waits with A's state and
// scope. Scenario: Ticks by hand don't release dependents. Scenario:
// Dependency complete: once a run of A is ready and complete, B starts.
func TestDependencyWaitsForTheWholeChange(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"a", "b"}, MaxParallel: 2})
	f.withTasks(scopedTasks, "a")
	f.dependsOn("b", "a")
	f.env.SetPlan(t, map[string]testenv.Action{"a:build:1": tick("1.1", "1.2")})
	if r := f.run(Request{Change: "a", Scope: "1"}); r.State != Ready || r.Complete {
		t.Fatalf("a: %s %v", r.State, r.Complete)
	}
	db := f.start(context.Background(), Request{Change: "b"})
	f.waitReason("b", "waiting on a (ready, tasks 1)")
	// A person ticks group 2 by hand: A's tasks.md is complete, its run
	// still says not complete, and B keeps waiting.
	p := TasksFile(f.worktree("a"), "a")
	testenv.WriteFile(t, p, strings.NewReplacer("- [ ] 2.1", "- [x] 2.1", "- [ ] 2.2", "- [x] 2.2").Replace(readFile(t, p)))
	time.Sleep(300 * time.Millisecond)
	if r := f.current("b"); r.State != Queued || r.Reason != "waiting on a (ready, tasks 1)" {
		t.Fatalf("b after ticks by hand: %s %q", r.State, r.Reason)
	}
	// The rest of A runs and passes review: B starts.
	if r := f.run(Request{Change: "a"}); r.State != Ready || !r.Complete {
		t.Fatalf("a whole: %s %v %q", r.State, r.Complete, r.Reason)
	}
	if res := <-db; res.err != nil || res.run.State != Ready {
		t.Fatalf("b: %+v %v", res.run, res.err)
	}
}

// Scenario: Dependency complete: a ready run recorded as complete releases
// B; a running scoped dependency shows its step and scope.
func TestDependencyReasons(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"a", "b"}, MaxParallel: 2})
	f.dependsOn("b", "a")
	l := &depLookup{runs: map[string]Run{}}
	cases := []struct {
		run         Run
		state, step string
	}{
		{Run{State: Ready}, "ready", ""},
		{Run{State: Ready, Scope: scopeAt("2"), Complete: true}, "ready", ""},
		{Run{State: Ready, Scope: scopeAt("1")}, "ready, tasks 1", ""},
		{Run{State: Running, Step: "build", Scope: scopeAt("2")}, "running", "build, tasks 2"},
		{Run{State: Queued, Step: "build", Scope: scopeAt("2"), Complete: true}, "queued, tasks 2", "build"},
	}
	for _, c := range cases {
		l.runs["a"] = c.run
		if st, step := l.lookup("a"); st != c.state || step != c.step {
			t.Errorf("%+v: %q %q, want %q %q", c.run, st, step, c.state, c.step)
		}
	}
	f.fakeRun("a", Run{State: Ready, Step: "", Round: 2, Scope: scopeAt("2"), Complete: true})
	if r := f.run(Request{Change: "b"}); r.State != Ready {
		t.Fatalf("b: %s %q", r.State, r.Reason)
	}
}

// Scenario: Next group ticked while the dependent checks. A is ready with
// scope 1 and not complete. Right after B reads A's run.json, A starts
// scope 2 and its builder ticks every remaining task, before B reads
// again. B stays queued: no single read shows A ready and complete. Only
// when A's review passes does B start.
func TestNextGroupTickedWhileTheDependentChecks(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"a", "b"}, MaxParallel: 2})
	f.withTasks(scopedTasks, "a")
	f.dependsOn("b", "a")
	review := f.gate("review")
	f.env.SetPlan(t, map[string]testenv.Action{
		"a:build:1":  tick("1.1", "1.2"),
		"a:build:2":  tick("2.1", "2.2"),
		"a:review:2": {WaitFor: review},
	})
	if r := f.run(Request{Change: "a", Scope: "1"}); r.State != Ready || r.Complete {
		t.Fatalf("a: %s %v", r.State, r.Complete)
	}

	var armed atomic.Bool
	var once sync.Once
	started := make(chan (<-chan result), 1)
	depReadHook = func() {
		if !armed.Load() {
			return
		}
		once.Do(func() {
			// Between B's reads: A starts scope 2 and its builder ticks
			// the rest; A waits in review.
			started <- f.start(context.Background(), Request{Change: "a", Scope: "next"})
			waitFor(t, "a in review with every task ticked", func() bool {
				r := f.current("a")
				return r != nil && r.State == Running && r.Step == "review" &&
					!strings.Contains(readFile(t, TasksFile(f.worktree("a"), "a")), "- [ ] 2.2")
			})
		})
	}
	t.Cleanup(func() { depReadHook = nil })

	db := f.start(context.Background(), Request{Change: "b"})
	f.waitReason("b", "waiting on a (ready, tasks 1)")
	armed.Store(true)
	f.waitReason("b", "waiting on a (review, tasks 2)")
	time.Sleep(300 * time.Millisecond)
	if r := f.current("b"); r.State != Queued {
		t.Fatalf("b started while a's scope 2 was in review: %s", r.State)
	}
	open_(t, review)
	if res := <-<-started; res.err != nil || res.run.State != Ready || !res.run.Complete || res.run.Round != 2 {
		t.Fatalf("a scope 2: %+v %v", res.run, res.err)
	}
	if res := <-db; res.err != nil || res.run.State != Ready {
		t.Fatalf("b: %+v %v", res.run, res.err)
	}
	wantStarts(t, f.env, "a build 1", "a review 1", "a build 2", "a review 2", "b build 1", "b review 1")
}

func scopeAt(spec string) *scope.Scope {
	return &scope.Scope{Spec: spec, Tasks: []string{spec + ".1"}}
}

// Scenario: Dependents in merge mode: A's change is complete and its run
// ready, but outside local mode only an archive releases B (the engine's
// half; archive on the base through a forge is add-forge-shipping's).
func TestDependentsInMergeMode(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"a", "b"}, MaxParallel: 2})
	f.dependsOn("b", "a")
	f.cfg.Ship = "merge"
	f.fakeRun("a", Run{State: Ready, Round: 2, Scope: scopeAt("2"), Complete: true})
	ctx, cancel := context.WithCancel(context.Background())
	db := f.start(ctx, Request{Change: "b"})
	f.waitReason("b", "waiting on a (ready, not archived)")
	time.Sleep(200 * time.Millisecond)
	if r := f.current("b"); r.State != Queued {
		t.Fatalf("b: %s", r.State)
	}
	cancel()
	if res := <-db; res.err != nil || res.run.State != Stopped {
		t.Fatalf("b: %+v %v", res.run, res.err)
	}
	// Archived on the base: B gets past its dependency. Its own task is
	// unticked and it has no scope, so in merge mode it blocks.
	if err := os.MkdirAll(filepath.Join(f.dir, "openspec", "changes", "archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	testenv.Git(t, f.dir, "mv", "openspec/changes/a", "openspec/changes/archive/2026-11-12-a")
	testenv.Git(t, f.dir, "commit", "-q", "-m", "archive a")
	if r := f.run(Request{Change: "b"}); r.State != Blocked || r.Reason != "tasks not ticked: 1.1" {
		t.Fatalf("b: %s %q", r.State, r.Reason)
	}
}
