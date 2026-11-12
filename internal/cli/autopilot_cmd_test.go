package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// approveAll approves and commits changes on main.
func (f *cliFixture) approveAll(ids ...string) {
	f.t.Helper()
	for _, id := range ids {
		f.mustApprove(id)
	}
	f.commit("approve " + strings.Join(ids, ", "))
}

func (f *cliFixture) pass(args ...string) cmdResult {
	f.t.Helper()
	res := f.tb(append([]string{"autopilot", "--once"}, args...)...)
	if res.code != 0 {
		f.t.Fatalf("autopilot --once %v: %d\n%s\n%s", args, res.code, res.stdout, res.stderr)
	}
	return res
}

func (f *cliFixture) noRun(id string) {
	f.t.Helper()
	if s, _ := f.state(id); s != "" {
		f.t.Errorf("%s has a run (%s)", id, s)
	}
}

// Scenario: Approval off.
func TestAutopilotApprovalOff(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a"}})
	res := f.tb("autopilot", "--once")
	if res.code == 0 || !strings.Contains(res.stderr, "autopilot needs approval to be required") {
		t.Errorf("%d %s", res.code, res.stderr)
	}
	f.noRun("a")
}

// Scenario: Interval too short. A bad scope is named too.
func TestAutopilotIntervalTooShort(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a"}, Extra: "\n[autopilot]\ninterval = \"1s\"\nscope = \"tasks\"\n"})
	res := f.tb("check")
	for _, want := range []string{".tuckerbox/config.toml", "autopilot.interval", "autopilot.scope"} {
		if res.code == 0 || !strings.Contains(res.stdout+res.stderr, want) {
			t.Errorf("check lacks %q: %d %s %s", want, res.code, res.stdout, res.stderr)
		}
	}
}

// Scenario: Second autopilot: the second names the first one's pid.
// Ctrl-C stops the first, and it leaves no marker behind.
func TestSecondAutopilot(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a"}, RequireApproval: true})
	cmd := exec.Command(tools.Tuckerbox, "autopilot")
	cmd.Dir = f.repo
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(f.repo, ".git", "tuckerbox", "autopilot.json")
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cmd.Process.Kill()
			t.Fatal("first autopilot wrote no marker")
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	res := f.tb("autopilot", "--once")
	if res.code == 0 || !strings.Contains(res.stderr, "already running") || !strings.Contains(res.stderr, "pid "+strconv.Itoa(cmd.Process.Pid)) {
		t.Errorf("second: %d %s", res.code, res.stderr)
	}
	if runtime.GOOS == "windows" {
		cmd.Process.Kill()
	} else {
		cmd.Process.Signal(os.Interrupt)
	}
	cmd.Wait()
	if runtime.GOOS != "windows" {
		if _, err := os.Stat(marker); err == nil {
			t.Error("marker left behind")
		}
	}
	// With the first gone, a new one runs.
	f.pass()
}

// Scenario: Dry run: both approved changes, in order, nothing started.
// Scenario: Unapproved change: never started.
func TestAutopilotDryRun(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login", "add-users", "proposed"}, MaxParallel: 3, RequireApproval: true})
	f.approveAll("add-login", "add-users")
	res := f.pass("--dry-run")
	want := "would start add-login --tasks next\nwould start add-users --tasks next\n"
	if res.stdout != want {
		t.Errorf("dry run:\n%s\nwant:\n%s", res.stdout, want)
	}
	for _, id := range []string{"add-login", "add-users", "proposed"} {
		f.noRun(id)
	}
	f.noWorktree("add-login")
}

// Scenario: Within max_parallel: one run is running, three approved
// changes have no run, max_parallel = 2: only the first is started.
// Scenario: Restart mid-run: a new autopilot starts nothing for runs that
// are running.
func TestAutopilotWithinMaxParallel(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a", "b", "c", "d"}, MaxParallel: 2, RequireApproval: true})
	f.approveAll("a", "b", "c", "d")
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: f.gate("build")}})
	if res := f.tb("run", "d"); res.code != 0 {
		t.Fatalf("run d: %s", res.stderr)
	}
	f.waitState("d", "running", "build")
	res := f.pass()
	if !strings.Contains(res.stdout, "start a --tasks next") || strings.Contains(res.stdout, " b") || strings.Contains(res.stdout, " c") {
		t.Errorf("pass:\n%s", res.stdout)
	}
	f.waitState("a", "running", "build")
	f.noRun("b")
	// No room: the next pass (a restarted autopilot) starts nothing and
	// leaves the running runs alone.
	if res := f.pass(); strings.TrimSpace(res.stdout) != "" {
		t.Errorf("second pass:\n%s", res.stdout)
	}
	f.noRun("b")
	if s, st := f.state("a"); s != "running" || st != "build" {
		t.Errorf("a: %s %s", s, st)
	}
	if r := f.runOf("a"); r.Scope == nil || r.Scope.Spec != "1" {
		t.Errorf("a's scope %+v", r.Scope)
	}
	f.open("build")
}

// Scenario: Dependency not met: B waits until A counts as met, then a
// later pass starts it.
func TestAutopilotDependencyNotMet(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a", "b"}, MaxParallel: 2, RequireApproval: true})
	testenv.WriteFile(t, filepath.Join(f.changeDir("b"), "tuckerbox.toml"), "depends_on = [\"a\"]\n")
	f.commit("b depends on a")
	f.approveAll("a", "b")
	f.env.SetPlan(t, map[string]testenv.Action{
		"a:build": {WaitFor: f.gate("a"), Tick: []string{"1.1"}},
		"build":   {Tick: []string{"1.1"}},
	})
	res := f.pass()
	if !strings.Contains(res.stdout, "start a ") || strings.Contains(res.stdout, "start b") {
		t.Errorf("pass 1:\n%s", res.stdout)
	}
	f.waitState("a", "running", "build")
	if res := f.pass(); strings.Contains(res.stdout, "start b") {
		t.Errorf("b started while a runs:\n%s", res.stdout)
	}
	f.open("a")
	f.waitState("a", "ready", "")
	if res := f.pass(); !strings.Contains(res.stdout, "start b --tasks next") {
		t.Errorf("pass 3:\n%s", res.stdout)
	}
	f.waitState("b", "ready", "")
}

// Scenario: Next group: a ready run with scope 1 and group 2 unticked is
// started with --tasks next, which resolves to 2, under the gate.
// Scenario: Whole changes: scope = "change" starts with no scope.
func TestAutopilotNextGroupAndWholeChanges(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	f.tasks("add-login", scopeTasks)
	f.approveAll("add-login")
	f.env.SetPlan(t, map[string]testenv.Action{
		"build:1": {Tick: []string{"1.1", "1.2", "1.3", "1.4"}},
		"build:2": {Tick: []string{"2.1", "2.2", "2.3"}},
	})
	f.pass()
	f.waitState("add-login", "ready", "")
	if r := f.runOf("add-login"); r.Scope.Spec != "1" || r.Complete {
		t.Fatalf("group 1: %+v complete %v", r.Scope, r.Complete)
	}
	res := f.pass()
	if !strings.Contains(res.stdout, "start add-login --tasks next") {
		t.Errorf("pass 2:\n%s", res.stdout)
	}
	f.waitState("add-login", "ready", "")
	waitFor(t, "group 2 ready", func() bool { r := f.runOf("add-login"); return r.Scope.Spec == "2" && r.State == "ready" })
	hist := f.tb("history", "add-login", "--json").stdout
	if !strings.Contains(hist, `"by": "autopilot"`) {
		t.Errorf("history lacks by autopilot:\n%s", hist)
	}

	g := newCLI(t, testenv.RepoOptions{Changes: []string{"whole"}, RequireApproval: true, Extra: "\n[autopilot]\nscope = \"change\"\n"})
	g.approveAll("whole")
	if res := g.pass(); !strings.Contains(res.stdout, "start whole\n") {
		t.Errorf("change mode:\n%s", res.stdout)
	}
	g.waitState("whole", "ready", "")
	if r := g.runOf("whole"); r.Scope != nil {
		t.Errorf("whole change run has scope %+v", r.Scope)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
