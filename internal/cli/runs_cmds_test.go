package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// tools holds the tuckerbox binary and the stand-ins, built once.
var tools *testenv.Tools

func TestMain(m *testing.M) {
	if os.Getenv(fakeSSHEnv) != "" {
		os.Exit(fakeSSH())
	}
	var err error
	tools, err = testenv.Build(true)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	tools.Remove()
	os.Exit(code)
}

type cliFixture struct {
	t    *testing.T
	env  *testenv.Env
	repo string
	// bare is the local shipping remote, when a test has one.
	bare string
}

// newCLI makes a scratch repository and an isolated environment. At the
// end of the test every gate opens and the runs are left to finish, so no
// detached process outlives the test.
func newCLI(t *testing.T, o testenv.RepoOptions) *cliFixture {
	t.Helper()
	f := &cliFixture{t: t, env: tools.Setup(t)}
	f.repo = tools.NewRepo(t, o)
	t.Cleanup(f.drain)
	return f
}

func (f *cliFixture) gate(name string) string { return filepath.Join(f.env.Home, "gate-"+name) }

func (f *cliFixture) open(name string) { testenv.WriteFile(f.t, f.gate(name), "go\n") }

func (f *cliFixture) drain() {
	for _, g := range []string{"a", "b", "c", "build", "review", "never"} {
		f.open(g)
	}
	// Wait until no run process holds its change lock; on Windows a
	// process that still has a file open would stop the scratch
	// directories being removed.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		busy := false
		for _, r := range f.status().Runs {
			st, err := statedir.Resolve(context.Background(), r.Worktree)
			if err == nil && runs.Active(st.RunLock()) {
				busy = true
			}
		}
		if !busy {
			if runtime.GOOS == "windows" {
				time.Sleep(500 * time.Millisecond) // let the processes exit
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	f.t.Error("runs still active at the end of the test")
}

type cmdResult struct {
	stdout, stderr string
	code           int
}

func (f *cliFixture) tb(args ...string) cmdResult {
	f.t.Helper()
	return tbIn(f.t, f.repo, args...)
}

func tbIn(t *testing.T, dir string, args ...string) cmdResult {
	t.Helper()
	cmd := exec.Command(tools.Tuckerbox, args...)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("tuckerbox %v: %v", args, err)
	}
	return cmdResult{out.String(), errOut.String(), code}
}

type statusDoc struct {
	Version int `json:"version"`
	Runs    []struct {
		Repo, Change, State, Step, Since, Reason, Worktree, Branch, Created string
		Round, Pid                                                          int
	} `json:"runs"`
}

func (f *cliFixture) status() statusDoc {
	f.t.Helper()
	res := f.tb("status", "--json")
	if res.code != 0 {
		f.t.Fatalf("status --json: exit %d: %s", res.code, res.stderr)
	}
	var doc statusDoc
	if err := json.Unmarshal([]byte(res.stdout), &doc); err != nil {
		f.t.Fatalf("status --json: %v\n%s", err, res.stdout)
	}
	return doc
}

// state returns change's state and step in status --json, or "" if absent.
func (f *cliFixture) state(change string) (string, string) {
	for _, r := range f.status().Runs {
		if r.Change == change {
			return r.State, r.Step
		}
	}
	return "", ""
}

func (f *cliFixture) waitState(change, state, step string) {
	f.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		s, st := f.state(change)
		if s == state && (step == "" || st == step) {
			return
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("timed out waiting for %s to be %s %s (is %s %s)", change, state, step, s, st)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Scenario: Invalid change. openspec validate fails: no worktree or run is
// made and the validation errors are shown.
func TestRunInvalidChange(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	testenv.WriteFile(t, filepath.Join(f.repo, "openspec", "changes", "fix-login", "INVALID"), "Requirement must have a scenario\n")
	res := f.tb("run", "fix-login")
	if res.code != 1 {
		t.Fatalf("exit %d", res.code)
	}
	if !strings.Contains(res.stderr, "Requirement must have a scenario") {
		t.Errorf("validation errors not shown: %s", res.stderr)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.repo), "repo-worktrees")); err == nil {
		t.Error("worktree directory made")
	}
	if out := testenv.Git(t, f.repo, "branch", "--list", "fix-login"); out != "" {
		t.Errorf("branch made: %s", out)
	}
	if len(f.status().Runs) != 0 {
		t.Error("run recorded")
	}
	// An unknown change fails the same way.
	if res := f.tb("run", "no-such-change"); res.code != 1 || !strings.Contains(res.stderr, "Unknown item") {
		t.Errorf("unknown change: %d %s", res.code, res.stderr)
	}
}

func TestRunForeground(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	res := f.tb("run", "--foreground", "fix-login")
	if res.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	for _, s := range []string{"step build round 1", "step review round 1", "ready"} {
		if !strings.Contains(res.stdout, s) {
			t.Errorf("output lacks %q:\n%s", s, res.stdout)
		}
	}
	if s, _ := f.state("fix-login"); s != "ready" {
		t.Errorf("state %s", s)
	}
}

// Two tuckerbox processes race for one change: exactly one runs it, the
// other refuses and points to the active run (formal/runs.tla,
// OneRunPerChange). Both also race to make the worktree.
func TestTwoProcessesRaceForOneChange(t *testing.T) {
	for i := 0; i < 3; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			f := newCLI(t, testenv.RepoOptions{Changes: []string{"race"}})
			f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: f.gate("build")}})
			ch := make(chan cmdResult, 2)
			start := make(chan struct{})
			for j := 0; j < 2; j++ {
				go func() {
					<-start
					ch <- f.tb("run", "--foreground", "race")
				}()
			}
			close(start)
			f.waitState("race", "running", "build")
			// The loser does not wait for the winner: it is refused while
			// the winner's build is still held at its gate.
			var results []cmdResult
			select {
			case r := <-ch:
				results = append(results, r)
			case <-time.After(20 * time.Second):
				t.Fatal("neither process returned while the change was running")
			}
			f.open("build")
			results = append(results, <-ch)
			var won, lost int
			for _, r := range results {
				switch {
				case r.code == 0 && strings.Contains(r.stdout, "ready"):
					won++
				case r.code == 1 && strings.Contains(r.stderr, "already has an active run") && strings.Contains(r.stderr, "tuckerbox status"):
					lost++
				default:
					t.Errorf("unexpected result: exit %d\n%s\n%s", r.code, r.stdout, r.stderr)
				}
			}
			if won != 1 || lost != 1 {
				t.Fatalf("won %d, lost %d", won, lost)
			}
			if got := f.env.Starts(t); strings.Join(got, ",") != "race build 1,race review 1" {
				t.Errorf("agent starts %v", got)
			}
		})
	}
}

// Scenario: Change already running (through the CLI, detached).
func TestRunRefusesActiveChange(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: f.gate("build")}})
	if res := f.tb("run", "fix-login"); res.code != 0 {
		t.Fatalf("first run: %d %s", res.code, res.stderr)
	}
	f.waitState("fix-login", "running", "build")
	res := f.tb("run", "fix-login")
	if res.code != 1 || !strings.Contains(res.stderr, "already has an active run") || !strings.Contains(res.stderr, "pid ") {
		t.Fatalf("second run: %d %s", res.code, res.stderr)
	}
	f.open("build")
	f.waitState("fix-login", "ready", "")
}

// run --all with max_parallel = 1: one process per change, detached (run
// returns while they work), the others queued and started in order.
func TestRunAllQueuesWithOneSlot(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a", "b", "c"}, MaxParallel: 1})
	testenv.AddChange(t, f.repo, "broken")
	testenv.WriteFile(t, filepath.Join(f.repo, "openspec", "changes", "broken", "INVALID"), "bad\n")
	f.env.SetPlan(t, map[string]testenv.Action{"a:build": {WaitFor: f.gate("a")}})

	res := f.tb("run", "--all")
	if res.code != 0 {
		t.Fatalf("run --all: %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "skipping broken") {
		t.Errorf("invalid change not reported: %s", res.stderr)
	}
	for _, c := range []string{"a", "b", "c"} {
		if !strings.Contains(res.stdout, c+": started") {
			t.Errorf("%s not started:\n%s", c, res.stdout)
		}
	}
	f.waitState("a", "running", "build")
	if s, _ := f.state("b"); s != "queued" {
		t.Errorf("b is %s", s)
	}
	if s, _ := f.state("c"); s != "queued" {
		t.Errorf("c is %s", s)
	}
	pids := map[int]bool{}
	for _, r := range f.status().Runs {
		pids[r.Pid] = true
	}
	if len(pids) != 3 {
		t.Errorf("want one process per change, pids %v", pids)
	}
	f.open("a")
	for _, c := range []string{"a", "b", "c"} {
		f.waitState(c, "ready", "")
	}
	want := "a build 1,a review 1,b build 1,b review 1,c build 1,c review 1"
	if got := strings.Join(f.env.Starts(t), ","); got != want {
		t.Errorf("order:\n%s\nwant\n%s", got, want)
	}
	// Ready changes are skipped by a second --all.
	res = f.tb("run", "--all")
	if res.code != 0 || !strings.Contains(res.stdout, "a: skipped") {
		t.Errorf("second --all: %d %s %s", res.code, res.stdout, res.stderr)
	}
}

// Scenario: Resume after a stop, through stop and resume.
func TestStopAndResume(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"review:1": {WaitFor: f.gate("never")}})
	if res := f.tb("run", "fix-login"); res.code != 0 {
		t.Fatalf("run: %d %s", res.code, res.stderr)
	}
	f.waitState("fix-login", "running", "review")
	res := f.tb("stop", "fix-login")
	if res.code != 0 || !strings.Contains(res.stdout, "stopped at step review") {
		t.Fatalf("stop: %d %s %s", res.code, res.stdout, res.stderr)
	}
	doc := f.status()
	if r := doc.Runs[0]; r.State != "stopped" || r.Step != "review" || !strings.Contains(r.Reason, "tuckerbox stop") {
		t.Fatalf("after stop: %+v", r)
	}
	if res := f.tb("stop", "fix-login"); res.code != 1 || !strings.Contains(res.stderr, "no active run") {
		t.Errorf("second stop: %d %s", res.code, res.stderr)
	}
	if res := f.tb("resume", "fix-login"); res.code != 0 {
		t.Fatalf("resume: %d %s", res.code, res.stderr)
	}
	f.waitState("fix-login", "ready", "")
	if got := strings.Join(f.env.Starts(t), ","); got != "fix-login build 1,fix-login review 1,fix-login review 2" {
		t.Errorf("starts %s", got)
	}
}

func TestRerunAndResumeRules(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	if res := f.tb("run", "--foreground", "fix-login"); res.code != 0 {
		t.Fatal(res.stderr)
	}
	if res := f.tb("resume", "fix-login"); res.code != 1 || !strings.Contains(res.stderr, "only a stopped or blocked run") {
		t.Errorf("resume of a ready run: %d %s", res.code, res.stderr)
	}
	res := f.tb("rerun", "--foreground", "fix-login", "build")
	if res.code != 0 {
		t.Fatalf("rerun: %d %s %s", res.code, res.stdout, res.stderr)
	}
	if got := strings.Join(f.env.Starts(t), ","); got != "fix-login build 1,fix-login review 1,fix-login build 2,fix-login review 2" {
		t.Errorf("starts %s", got)
	}
	// From outside the repository, the machine index finds the run.
	if res := tbIn(t, t.TempDir(), "logs", "fix-login", "build"); res.code != 0 || !strings.Contains(res.stdout, "fakeagent: fix-login build round 2") {
		t.Errorf("logs from outside: %d %s %s", res.code, res.stdout, res.stderr)
	}
}

// Scenario: Mixed runs. One change runs its review step and another is
// blocked: both appear, the blocked one with its reason.
func TestStatusMixedRuns(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a", "b"}, MaxParallel: 2})
	f.env.SetPlan(t, map[string]testenv.Action{
		"a:review": {WaitFor: f.gate("a")},
		"b:build":  {Status: "blocked", Summary: "needs a product decision"},
	})
	if res := f.tb("run", "a", "b"); res.code != 0 {
		t.Fatalf("run: %d %s", res.code, res.stderr)
	}
	f.waitState("a", "running", "review")
	f.waitState("b", "blocked", "")

	res := f.tb("status")
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	// The table, then the blocked run's question (blocked-answers).
	table, questions, _ := strings.Cut(res.stdout, "\n\n")
	lines := strings.Split(strings.TrimSpace(table), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "REPO") {
		t.Fatalf("table:\n%s", res.stdout)
	}
	if !strings.HasPrefix(questions, "b asks (step build, round 1):\n  needs a product decision\n") {
		t.Errorf("questions:\n%s", questions)
	}
	if !strings.Contains(lines[1], "a ") || !strings.Contains(lines[1], "running") || !strings.Contains(lines[1], "review") {
		t.Errorf("row a: %s", lines[1])
	}
	if !strings.Contains(lines[2], "blocked") || !strings.Contains(lines[2], "needs a product decision") {
		t.Errorf("row b: %s", lines[2])
	}
	// Times are in the local zone: the table shows the zone's name.
	zone, _ := time.Now().Zone()
	if !strings.Contains(lines[1], zone) {
		t.Errorf("row a lacks local zone %s: %s", zone, lines[1])
	}

	doc := f.status()
	if doc.Version != 1 || len(doc.Runs) != 2 {
		t.Fatalf("json: %+v", doc)
	}
	for _, r := range doc.Runs {
		ts, err := time.Parse(time.RFC3339, r.Since)
		if err != nil || time.Since(ts) > time.Hour || !strings.ContainsAny(r.Since[len(r.Since)-6:], "+-Z") {
			t.Errorf("since %q: %v", r.Since, err)
		}
		if r.Repo != f.repo || r.Worktree == "" || r.Branch != r.Change {
			t.Errorf("run %+v", r)
		}
	}
	if b := doc.Runs[1]; b.Change != "b" || b.State != "blocked" || !strings.Contains(b.Reason, "needs a product decision") {
		t.Errorf("b: %+v", b)
	}
	f.open("a")
	f.waitState("a", "ready", "")
}

// Scenario: Dashboard reads status. A JSON document with version and a
// runs array, and exit status 0 even when there are no runs.
func TestStatusJSONWithNoRuns(t *testing.T) {
	tools.Setup(t)
	res := tbIn(t, t.TempDir(), "status", "--json")
	if res.code != 0 {
		t.Fatalf("exit %d: %s", res.code, res.stderr)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(res.stdout), &doc); err != nil {
		t.Fatal(err)
	}
	if string(doc["version"]) != "1" || string(doc["runs"]) != "[]" {
		t.Errorf("doc: %s", res.stdout)
	}
	if res := tbIn(t, t.TempDir(), "status"); res.code != 0 || !strings.Contains(res.stdout, "no runs") {
		t.Errorf("table: %d %s", res.code, res.stdout)
	}
}

// Scenario: Following a running step. tuckerbox logs <change> --follow while
// the build step runs shows its output as it is written, then follows the
// run to the next step.
func TestLogsFollow(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: f.gate("build")}})
	if res := f.tb("run", "fix-login"); res.code != 0 {
		t.Fatal(res.stderr)
	}
	f.waitState("fix-login", "running", "build")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, tools.Tuckerbox, "logs", "fix-login", "--follow")
	cmd.Dir = f.repo
	out := &lockedBuffer{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(out.String(), "fakeagent: fix-login build round 1") {
		if time.Now().After(deadline) {
			t.Fatalf("build output did not appear while it ran:\n%s", out)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if s, step := f.state("fix-login"); s != "running" || step != "build" {
		t.Fatalf("build no longer running: %s %s", s, step)
	}
	f.open("build")
	if err := cmd.Wait(); err != nil {
		t.Fatalf("logs --follow: %v\n%s", err, out)
	}
	for _, s := range []string{"==> tuckerbox: step review <==", "fakeagent: fix-login review round 1"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("follow output lacks %q:\n%s", s, out)
		}
	}
	res := f.tb("logs", "fix-login", "review")
	if res.code != 0 || !strings.Contains(res.stdout, "accepted result: done") {
		t.Errorf("logs review: %d %s %s", res.code, res.stdout, res.stderr)
	}
	if res := f.tb("logs", "fix-login", "deploy"); res.code != 1 || !strings.Contains(res.stderr, "no log yet") {
		t.Errorf("logs of a step without a log: %d %s", res.code, res.stderr)
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestRunsUsage(t *testing.T) {
	a, _, errOut := app()
	for _, args := range [][]string{
		{"run"}, {"run", "--all", "x"}, {"run", "--foreground", "a", "b"}, {"run", "--bogus", "x"},
		{"stop"}, {"rerun", "x"}, {"logs"}, {"status", "x"},
	} {
		errOut.Reset()
		if code := a.Run(args); code != 2 {
			t.Errorf("%v: exit %d, want 2 (%s)", args, code, errOut)
		}
	}
}

func TestParseArgs(t *testing.T) {
	var all bool
	var step string
	pos, err := parseArgs([]string{"a", "--all", "--step=x", "b", "--", "--c"},
		map[string]*bool{"all": &all}, map[string]*string{"step": &step})
	if err != nil || !all || step != "x" || strings.Join(pos, ",") != "a,b,--c" {
		t.Errorf("%v %v %q %v", pos, all, step, err)
	}
	if _, err := parseArgs([]string{"--step"}, nil, map[string]*string{"step": &step}); err == nil {
		t.Error("missing value accepted")
	}
}
