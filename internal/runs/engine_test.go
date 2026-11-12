package runs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

var tools *testenv.Tools

func TestMain(m *testing.M) {
	var err error
	tools, err = testenv.Build(false)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	tools.Remove()
	os.Exit(code)
}

type fixture struct {
	t    *testing.T
	env  *testenv.Env
	dir  string
	repo Repo
	cfg  *config.Config
}

func setup(t *testing.T, o testenv.RepoOptions) *fixture {
	t.Helper()
	env := tools.Setup(t)
	dir := tools.NewRepo(t, o)
	return open(t, env, dir)
}

func open(t *testing.T, env *testenv.Env, dir string) *fixture {
	t.Helper()
	ctx := context.Background()
	repo, err := OpenRepo(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	opts, err := config.Discover(config.SystemEnv(), dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(opts)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, env: env, dir: dir, repo: repo, cfg: cfg}
}

func (f *fixture) engine() *Engine {
	return &Engine{
		Repo: f.repo, Config: f.cfg, StateHome: f.env.StateHome,
		Poll:  20 * time.Millisecond,
		Agent: agent.Options{Poll: 20 * time.Millisecond, StopGrace: 5 * time.Second},
		Log:   &testLog{t: f.t},
	}
}

type testLog struct{ t *testing.T }

func (l *testLog) Write(p []byte) (int, error) {
	l.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

func (f *fixture) run(req Request) *Run {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	r, err := f.engine().Run(ctx, req)
	if err != nil {
		f.t.Fatalf("run %s: %v", req.Change, err)
	}
	return r
}

// start runs req in the background and returns a channel with its result.
// Like tuckerbox run, it returns once the run holds its change lock and has
// joined the queue (or has ended).
func (f *fixture) start(ctx context.Context, req Request) <-chan result {
	ch := make(chan result, 1)
	locked := make(chan struct{})
	e := f.engine()
	e.OnLocked = func(Workspace, *Run) { close(locked) }
	go func() {
		r, err := e.Run(ctx, req)
		ch <- result{r, err}
	}()
	select {
	case <-locked:
		return ch
	case res := <-ch:
		out := make(chan result, 1)
		out <- res
		return out
	}
}

type result struct {
	run *Run
	err error
}

func (f *fixture) worktree(change string) string {
	return f.repo.WorktreePath(f.cfg.Worktree, change)
}

func (f *fixture) state(change string) statedir.Dir {
	f.t.Helper()
	st, err := statedir.Resolve(context.Background(), f.worktree(change))
	if err != nil {
		f.t.Fatal(err)
	}
	return st
}

// current reads run.json, or nil before the run made it.
func (f *fixture) current(change string) *Run {
	if _, err := os.Stat(f.worktree(change)); err != nil {
		return nil
	}
	st, err := statedir.Resolve(context.Background(), f.worktree(change))
	if err != nil {
		return nil
	}
	r, err := ReadRun(st.RunFile())
	if err != nil {
		return nil
	}
	return r
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (f *fixture) waitState(change string, s State, step string) {
	f.t.Helper()
	waitFor(f.t, fmt.Sprintf("%s %s %s", change, s, step), func() bool {
		r := f.current(change)
		return r != nil && r.State == s && (step == "" || r.Step == step)
	})
}

func (f *fixture) gate(name string) string { return filepath.Join(f.env.Home, "gate-"+name) }

func open_(t *testing.T, p string) { testenv.WriteFile(t, p, "go\n") }

func wantStarts(t *testing.T, env *testenv.Env, want ...string) {
	t.Helper()
	got := env.Starts(t)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("agent starts:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Scenario: First run of a change. The change has no worktree yet:
// Tuckerbox creates the branch and worktree and starts the first step
// there. Scenario: Run completes locally: the branch holds the commits, the
// worktree is kept, and the state is ready.
func TestFirstRunAndLocalShip(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Touch: "login.go"}})

	r := f.run(Request{Change: "fix-login"})
	if r.State != Ready || r.Reason != "" || r.Step != "" {
		t.Fatalf("run: state %s step %q reason %q", r.State, r.Step, r.Reason)
	}
	wt := f.worktree("fix-login")
	if want := filepath.Join(filepath.Dir(f.dir), "repo-worktrees", "fix-login"); wt != want {
		t.Errorf("worktree %s, want %s", wt, want)
	}
	if b := testenv.Git(t, wt, "symbolic-ref", "--short", "HEAD"); b != "fix-login" {
		t.Errorf("branch %s", b)
	}
	if r.Base != "main" {
		t.Errorf("base %q, want main (no remote)", r.Base)
	}
	wantStarts(t, f.env, "fix-login build 1", "fix-login review 1")
	// The agent's work is committed on the branch; nothing is left over.
	if s := testenv.Git(t, wt, "status", "--porcelain"); s != "" {
		t.Errorf("worktree not clean after local ship:\n%s", s)
	}
	if files := testenv.Git(t, f.dir, "show", "--name-only", "--format=", "fix-login"); !strings.Contains(files, "login.go") {
		t.Errorf("last commit on fix-login lacks login.go: %s", files)
	}
	if n := testenv.Git(t, f.dir, "rev-list", "--count", "main..fix-login"); n != "1" {
		t.Errorf("commits on the branch: %s", n)
	}
	// Nothing was pushed (there is no remote) and main is untouched.
	if _, err := os.Stat(filepath.Join(f.dir, "login.go")); err == nil {
		t.Error("login.go appeared in the main checkout")
	}
	if len(r.History) != 2 || r.History[0].Step != "build" || r.History[1].Step != "review" {
		t.Errorf("history %+v", r.History)
	}
	ix, err := ReadIndex(f.env.StateHome)
	if err != nil || len(ix.Runs) != 1 || ix.Runs[0].Change != "fix-login" || ix.Runs[0].Worktree != wt {
		t.Errorf("index %+v %v", ix, err)
	}
	// The pid file and stop file are gone; the lock is free.
	if _, err := os.Stat(f.state("fix-login").PidFile()); err == nil {
		t.Error("pid file left behind")
	}
	if Active(f.state("fix-login").RunLock()) {
		t.Error("change lock still held")
	}
}

// Scenario: Existing worktree with uncommitted changes. Tuckerbox resumes
// in it without resetting, cleaning or stashing anything.
func TestExistingWorktreeWithUncommittedWork(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	wt := f.worktree("fix-login")
	testenv.Git(t, f.dir, "worktree", "add", "-q", "-b", "fix-login", wt, "main")
	testenv.WriteFile(t, filepath.Join(wt, "README.md"), "# edited by hand\n")
	testenv.WriteFile(t, filepath.Join(wt, "notes.txt"), "untracked work\n")
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Status: "blocked", Summary: "need a decision"}})

	r := f.run(Request{Change: "fix-login"})
	if r.State != Blocked {
		t.Fatalf("state %s", r.State)
	}
	if got := readFile(t, filepath.Join(wt, "README.md")); got != "# edited by hand\n" {
		t.Errorf("README.md changed: %q", got)
	}
	if got := readFile(t, filepath.Join(wt, "notes.txt")); got != "untracked work\n" {
		t.Errorf("notes.txt changed: %q", got)
	}
	if s := testenv.Git(t, wt, "status", "--porcelain"); !strings.Contains(s, "README.md") || !strings.Contains(s, "notes.txt") {
		t.Errorf("uncommitted work not left as it was:\n%s", s)
	}
	if s := testenv.Git(t, f.dir, "stash", "list"); s != "" {
		t.Errorf("stash used: %s", s)
	}
	if wts, _ := f.repo.Worktrees(context.Background()); len(wts) != 2 {
		t.Errorf("worktrees %v", wts)
	}
}

// Scenario: Reviewer rejects. The build step starts again and its brief
// includes the review's findings.
func TestReviewerRejects(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"review:1": {
		Status: "rejected", Summary: "not yet",
		Findings: []testenv.Finding{{Severity: "high", File: "login.go", Detail: "password is logged"}},
	}})
	r := f.run(Request{Change: "fix-login"})
	if r.State != Ready {
		t.Fatalf("state %s: %s", r.State, r.Reason)
	}
	wantStarts(t, f.env, "fix-login build 1", "fix-login review 1", "fix-login build 2", "fix-login review 2")
	brief := readFile(t, f.state("fix-login").Brief("build", 2))
	for _, s := range []string{"Findings from earlier rounds", "review, round 1: rejected", "password is logged"} {
		if !strings.Contains(brief, s) {
			t.Errorf("build round 2 brief lacks %q", s)
		}
	}
	if b1 := readFile(t, f.state("fix-login").Brief("build", 1)); strings.Contains(b1, "password is logged") {
		t.Error("round 1 brief has findings")
	}
}

// A reviewer that cannot write outside its worktree (a read-only sandbox)
// reports through its final message. The engine records the result, writes
// the result file and loops back to build with the findings.
func TestReviewerReportsInFinalMessage(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{
		"review:1": {
			Status: "rejected", Summary: "not yet", Message: true,
			Findings: []testenv.Finding{{Severity: "high", File: "login.go", Detail: "password is logged"}},
		},
		"review": {Message: true},
	})
	r := f.run(Request{Change: "fix-login"})
	if r.State != Ready {
		t.Fatalf("state %s: %s", r.State, r.Reason)
	}
	wantStarts(t, f.env, "fix-login build 1", "fix-login review 1", "fix-login build 2", "fix-login review 2")
	if brief := readFile(t, f.state("fix-login").Brief("build", 2)); !strings.Contains(brief, "password is logged") {
		t.Error("build round 2 brief lacks the findings from the final message")
	}
	if res := readFile(t, f.state("fix-login").Result("review", 1)); !strings.Contains(res, `"rejected"`) {
		t.Errorf("engine did not write the review result file:\n%s", res)
	}
}

// Scenario: Agent exits without a result. The step is failed with the exit
// status and the tail of its output.
func TestAgentExitsWithoutResult(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Status: "none", Exit: 3}})
	r := f.run(Request{Change: "fix-login"})
	if r.State != Failed || r.Step != "build" {
		t.Fatalf("state %s step %s", r.State, r.Step)
	}
	if !strings.Contains(r.Reason, "exited with status 3") {
		t.Errorf("reason %q", r.Reason)
	}
	if !strings.Contains(r.Tail, "giving up without a result") {
		t.Errorf("tail %q", r.Tail)
	}
	wantStarts(t, f.env, "fix-login build 1")
}

// Scenario: Limit reached. The review step rejects for the third time with
// max_rounds = 3: the run is blocked with reason "review limit reached"
// and no further step starts.
func TestReviewLimitReached(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"review": {Status: "rejected", Summary: "still wrong"}})
	r := f.run(Request{Change: "fix-login"})
	if r.State != Blocked || !strings.Contains(r.Reason, "review limit reached") {
		t.Fatalf("state %s reason %q", r.State, r.Reason)
	}
	wantStarts(t, f.env,
		"fix-login build 1", "fix-login review 1",
		"fix-login build 2", "fix-login review 2",
		"fix-login build 3", "fix-login review 3")
}

// A blocked agent pauses the run until a person resumes it.
func TestBlockedThenResume(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"build:1": {Status: "blocked", Summary: "which database?"}})
	r := f.run(Request{Change: "fix-login"})
	if r.State != Blocked || !strings.Contains(r.Reason, "which database?") {
		t.Fatalf("state %s reason %q", r.State, r.Reason)
	}
	r = f.run(Request{Change: "fix-login", Mode: Resume})
	if r.State != Ready {
		t.Fatalf("after resume: %s %s", r.State, r.Reason)
	}
	wantStarts(t, f.env, "fix-login build 1", "fix-login build 2", "fix-login review 2")
}

// A failed step stops the run; resume refuses it, rerun starts the step again.
func TestFailedThenRerun(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"build:1": {Status: "failed", Summary: "tests do not compile"}})
	r := f.run(Request{Change: "fix-login"})
	if r.State != Failed || !strings.Contains(r.Reason, "tests do not compile") {
		t.Fatalf("state %s reason %q", r.State, r.Reason)
	}
	_, err := f.engine().Run(context.Background(), Request{Change: "fix-login", Mode: Resume})
	var se *StateError
	if !errors.As(err, &se) || se.State != Failed {
		t.Fatalf("resume of a failed run: %v", err)
	}
	r = f.run(Request{Change: "fix-login", Mode: Rerun, Step: "build"})
	if r.State != Ready {
		t.Fatalf("after rerun: %s %s", r.State, r.Reason)
	}
}

// A stale result from an earlier round is ignored; the run goes on with
// the current round's result.
func TestStaleResultIgnored(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{
		"review:1": {Status: "rejected", Summary: "again"},
		"build:2":  {StaleFirst: true},
	})
	r := f.run(Request{Change: "fix-login"})
	if r.State != Ready {
		t.Fatalf("state %s: %s", r.State, r.Reason)
	}
	log := readFile(t, f.state("fix-login").Log("build"))
	if !strings.Contains(log, "stale result from round 1") {
		t.Errorf("build log does not record the stale result:\n%s", log)
	}
}

// Scenario: Resume after a stop. A run stopped during its review step is
// resumed: the review step starts again in the same worktree. Stop does
// not alter files in the worktree.
func TestStopDuringReviewThenResume(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	gate := f.gate("review")
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Touch: "login.go"}, "review": {WaitFor: gate}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := f.start(ctx, Request{Change: "fix-login"})
	f.waitState("fix-login", Running, "review")
	wt := f.worktree("fix-login")
	before := testenv.Git(t, wt, "status", "--porcelain")
	cancel()
	res := <-done
	if res.err != nil || res.run.State != Stopped || res.run.Step != "review" {
		t.Fatalf("after stop: %+v %v", res.run, res.err)
	}
	if after := testenv.Git(t, wt, "status", "--porcelain"); after != before {
		t.Errorf("stop changed the worktree:\n%s\nwas:\n%s", after, before)
	}
	if got := readFile(t, filepath.Join(wt, "login.go")); got != "build round 1\n" {
		t.Errorf("login.go %q", got)
	}
	open_(t, gate)
	r := f.run(Request{Change: "fix-login", Mode: Resume})
	if r.State != Ready {
		t.Fatalf("after resume: %s %s", r.State, r.Reason)
	}
	wantStarts(t, f.env, "fix-login build 1", "fix-login review 1", "fix-login review 2")
	if r.Worktree != wt {
		t.Errorf("resumed in %s", r.Worktree)
	}
}

// tuckerbox stop works through the stop file too (the way on Windows).
func TestStopFile(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: f.gate("never")}})
	done := f.start(context.Background(), Request{Change: "fix-login"})
	f.waitState("fix-login", Running, "build")
	testenv.WriteFile(t, StopFile(f.state("fix-login")), "")
	res := <-done
	if res.err != nil || res.run.State != Stopped || !strings.Contains(res.run.Reason, "tuckerbox stop") {
		t.Fatalf("after stop file: %+v %v", res.run, res.err)
	}
	if _, err := os.Stat(StopFile(f.state("fix-login"))); err == nil {
		t.Error("stop file left behind")
	}
}

// Rerun starts again from the named step and leaves the worktree's files.
func TestRerunFromStep(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	r := f.run(Request{Change: "fix-login"})
	if r.State != Ready {
		t.Fatal(r.State)
	}
	// A ready run is not started again by run.
	_, err := f.engine().Run(context.Background(), Request{Change: "fix-login"})
	var se *StateError
	if !errors.As(err, &se) || se.State != Ready {
		t.Fatalf("run of a ready change: %v", err)
	}
	wt := f.worktree("fix-login")
	testenv.WriteFile(t, filepath.Join(wt, "scratch.txt"), "mine\n")
	r = f.run(Request{Change: "fix-login", Mode: Rerun, Step: "review"})
	if r.State != Ready {
		t.Fatalf("rerun: %s %s", r.State, r.Reason)
	}
	wantStarts(t, f.env, "fix-login build 1", "fix-login review 1", "fix-login review 2")
	if got := readFile(t, filepath.Join(wt, "scratch.txt")); got != "mine\n" {
		t.Errorf("scratch.txt %q", got)
	}
	_, err = f.engine().Run(context.Background(), Request{Change: "fix-login", Mode: Rerun, Step: "deploy"})
	if err == nil || !strings.Contains(err.Error(), `step "deploy" is not in the workflow`) {
		t.Errorf("rerun of an unknown step: %v", err)
	}
}

// Scenario: Change already running. Tuckerbox refuses and points to the
// active run.
func TestChangeAlreadyRunning(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	gate := f.gate("build")
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: gate}})
	done := f.start(context.Background(), Request{Change: "fix-login"})
	f.waitState("fix-login", Running, "build")

	_, err := f.engine().Run(context.Background(), Request{Change: "fix-login"})
	var ae *ActiveError
	if !errors.As(err, &ae) || !errors.Is(err, ErrLocked) {
		t.Fatalf("second run: %v", err)
	}
	if ae.Pid != os.Getpid() || ae.Worktree != f.worktree("fix-login") {
		t.Errorf("active run: %+v", ae)
	}
	if !strings.Contains(err.Error(), "tuckerbox status") {
		t.Errorf("message does not point to the run: %v", err)
	}
	open_(t, gate)
	if res := <-done; res.err != nil || res.run.State != Ready {
		t.Fatalf("first run: %+v %v", res.run, res.err)
	}
	// Exactly one run's steps happened.
	wantStarts(t, f.env, "fix-login build 1", "fix-login review 1")
}

// With max_parallel = 1 a second change queues until the first ends.
func TestQueueWithOneSlot(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"a", "b"}})
	gate := f.gate("a")
	f.env.SetPlan(t, map[string]testenv.Action{"a:build": {WaitFor: gate}})
	da := f.start(context.Background(), Request{Change: "a"})
	f.waitState("a", Running, "build")
	db := f.start(context.Background(), Request{Change: "b"})
	f.waitState("b", Queued, "")
	time.Sleep(200 * time.Millisecond)
	if r := f.current("b"); r.State != Queued {
		t.Fatalf("b is %s while a holds the only slot", r.State)
	}
	open_(t, gate)
	for _, ch := range []<-chan result{da, db} {
		if res := <-ch; res.err != nil || res.run.State != Ready {
			t.Fatalf("%+v %v", res.run, res.err)
		}
	}
	wantStarts(t, f.env, "a build 1", "a review 1", "b build 1", "b review 1")
}

// Scenario: More changes than slots. max_parallel = 2 and four changes: two
// run, two are queued, and each queued run starts, in the order requested,
// when a running one ends or pauses.
func TestMoreChangesThanSlots(t *testing.T) {
	ids := []string{"c1", "c2", "c3", "c4"}
	f := setup(t, testenv.RepoOptions{Changes: ids, MaxParallel: 2})
	plan := map[string]testenv.Action{}
	for _, id := range ids {
		plan[id+":build"] = testenv.Action{WaitFor: f.gate(id)}
	}
	plan["c1:build"] = testenv.Action{WaitFor: f.gate("c1"), Status: "blocked", Summary: "pause"}
	f.env.SetPlan(t, plan)

	// Requested in order, each once the one before has joined the queue,
	// as tuckerbox run does.
	chans := map[string]<-chan result{}
	for i, id := range ids {
		chans[id] = f.start(context.Background(), Request{Change: id})
		if i < 2 {
			f.waitState(id, Running, "build")
		} else {
			f.waitState(id, Queued, "")
		}
	}

	count := func(s State) int {
		n := 0
		for _, id := range ids {
			if r := f.current(id); r != nil && r.State == s {
				n++
			}
		}
		return n
	}
	if count(Running) != 2 || count(Queued) != 2 {
		t.Fatalf("running %d, queued %d", count(Running), count(Queued))
	}
	// c1 pauses (blocked): c3, the first queued, starts; c4 still waits.
	open_(t, f.gate("c1"))
	f.waitState("c3", Running, "build")
	if r := f.current("c4"); r.State != Queued {
		t.Errorf("c4 is %s, want queued (c3 was requested first)", r.State)
	}
	if res := <-chans["c1"]; res.run.State != Blocked {
		t.Errorf("c1 %s", res.run.State)
	}
	open_(t, f.gate("c2"))
	f.waitState("c4", Running, "build")
	open_(t, f.gate("c3"))
	open_(t, f.gate("c4"))
	for _, id := range ids[1:] {
		if res := <-chans[id]; res.err != nil || res.run.State != Ready {
			t.Errorf("%s: %+v %v", id, res.run, res.err)
		}
	}
}

// A run queued for a slot can be stopped.
func TestStopWhileQueued(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"a", "b"}})
	gate := f.gate("a")
	f.env.SetPlan(t, map[string]testenv.Action{"a:build": {WaitFor: gate}})
	da := f.start(context.Background(), Request{Change: "a"})
	f.waitState("a", Running, "build")
	ctx, cancel := context.WithCancel(context.Background())
	db := f.start(ctx, Request{Change: "b"})
	f.waitState("b", Queued, "")
	cancel()
	if res := <-db; res.err != nil || res.run.State != Stopped || res.run.Step != "build" {
		t.Fatalf("b: %+v %v", res.run, res.err)
	}
	open_(t, gate)
	<-da
	wantStarts(t, f.env, "a build 1", "a review 1")
}

// Scenario: Invalid change... is checked by the CLI (openspec validate) before
// any engine runs; the engine itself refuses a change that is not on the
// base branch, and makes no worktree.
func TestChangeNotOnBase(t *testing.T) {
	f := setup(t, testenv.RepoOptions{})
	testenv.AddChange(t, f.dir, "uncommitted")
	_, err := f.engine().Run(context.Background(), Request{Change: "uncommitted"})
	if err == nil || !strings.Contains(err.Error(), "not on the base branch main") {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(f.worktree("uncommitted")); err == nil {
		t.Error("worktree made")
	}
	if out := testenv.Git(t, f.dir, "branch", "--list", "uncommitted"); out != "" {
		t.Errorf("branch made: %s", out)
	}
}

// By default the base is the remote's default branch after a fetch.
func TestBaseIsRemoteDefaultBranch(t *testing.T) {
	env := tools.Setup(t)
	upstream := tools.NewRepo(t, testenv.RepoOptions{})
	bare := filepath.Join(t.TempDir(), "origin.git")
	testenv.Git(t, upstream, "clone", "-q", "--bare", upstream, bare)
	clone := filepath.Join(t.TempDir(), "clone")
	testenv.Git(t, upstream, "clone", "-q", bare, clone)
	testenv.Git(t, clone, "config", "user.name", "Test")
	testenv.Git(t, clone, "config", "user.email", "test@example.invalid")
	// The change reaches the remote after the clone: only a fetch finds it.
	testenv.AddChange(t, upstream, "late")
	testenv.Git(t, upstream, "add", "-A")
	testenv.Git(t, upstream, "commit", "-q", "-m", "late change")
	testenv.Git(t, upstream, "push", "-q", bare, "main")

	f := open(t, env, clone)
	r := f.run(Request{Change: "late"})
	if r.State != Ready || r.Base != "origin/main" {
		t.Fatalf("state %s base %q reason %q", r.State, r.Base, r.Reason)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
