package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// kill crashes id's run: its agent and its engine process die without
// recording a final state, as on a reboot.
func (f *cliFixture) kill(id string) {
	f.t.Helper()
	r := f.runOf(id)
	for _, pid := range []int{r.AgentPid, r.Pid} {
		if pid > 0 {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		}
	}
	st, err := statedir.Resolve(context.Background(), f.worktreeOf(id))
	if err != nil {
		f.t.Fatal(err)
	}
	waitFor(f.t, id+"'s lock free", func() bool { return !runs.Active(st.RunLock()) })
	if r := f.runOf(id); r.State != runs.Running && r.State != runs.Queued {
		f.t.Fatalf("%s is %s after the kill", id, r.State)
	}
}

func (f *cliFixture) waitRound(id, step string, round int) {
	f.t.Helper()
	waitFor(f.t, id+" "+step+" running", func() bool {
		r := f.runOf(id)
		return r.State == runs.Running && r.Step == step && r.Round == round && runs.ReadPid(mustState(f.t, f.worktreeOf(id))) > 0
	})
}

func mustState(t *testing.T, wt string) statedir.Dir {
	t.Helper()
	st, err := statedir.Resolve(context.Background(), wt)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// Scenario: Machine restarted: the first pass resumes the build in a new
// round. Scenario: Crashes twice across rounds: after autopilot's resume
// crashes too, no pass resumes it, also after a restart, and it is
// reported. Scenario: A person renews the budget: after tuckerbox resume,
// a later crash is resumed once again.
func TestAutopilotCrashBudget(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	f.approveAll("add-login")
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: f.gate("never")}})
	f.pass()
	f.waitRound("add-login", "build", 1)
	f.kill("add-login")

	res := f.pass()
	if !strings.Contains(res.stdout, "resume add-login (its run crashed)") {
		t.Fatalf("pass after crash:\n%s", res.stdout)
	}
	f.waitRound("add-login", "build", 2)
	f.kill("add-login")
	for i := 0; i < 2; i++ { // two autopilots: the budget is in history
		res = f.pass()
		if strings.Contains(res.stdout, "resume add-login") || !strings.Contains(res.stdout, "add-login needs you: its run crashed again") {
			t.Fatalf("pass %d after the second crash:\n%s", i, res.stdout)
		}
	}
	if r := f.runOf("add-login"); r.Round != 2 {
		t.Errorf("round %d", r.Round)
	}

	if res := f.tb("resume", "add-login"); res.code != 0 {
		t.Fatalf("person's resume: %s", res.stderr)
	}
	f.waitRound("add-login", "build", 3)
	f.kill("add-login")
	if res := f.pass(); !strings.Contains(res.stdout, "resume add-login") {
		t.Errorf("after a person's resume:\n%s", res.stdout)
	}
	f.waitRound("add-login", "build", 4)
	f.kill("add-login")
}

// Scenario: Progress renews the budget: autopilot resumed a crashed
// build, the build reported done, and the review step later crashes: the
// next pass resumes the review.
func TestAutopilotProgressRenewsTheBudget(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	f.approveAll("add-login")
	f.env.SetPlan(t, map[string]testenv.Action{
		"build:1": {WaitFor: f.gate("never")},
		"build":   {Tick: []string{"1.1"}},
		"review":  {WaitFor: f.gate("never")},
	})
	f.pass()
	f.waitRound("add-login", "build", 1)
	f.kill("add-login")
	f.pass()
	f.waitRound("add-login", "review", 2)
	f.kill("add-login")
	if res := f.pass(); !strings.Contains(res.stdout, "resume add-login") {
		t.Errorf("review crash:\n%s", res.stdout)
	}
	f.waitRound("add-login", "review", 3)
	f.kill("add-login")
}

// Scenario: Stale approval from the run's own step: the build edited
// design.md and the run crashed; the pass resumes it. Scenario: Unapproved
// while crashed: once the owner withdraws the approval, no pass resumes it.
func TestAutopilotStaleAndUnapprovedCrash(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	f.approveAll("add-login")
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: f.gate("never")}})
	f.pass()
	f.waitRound("add-login", "build", 1)
	// The build step edits design.md (the test does it for the agent).
	testenv.WriteFile(t, filepath.Join(f.worktreeOf("add-login"), "openspec", "changes", "add-login", "design.md"), "# Design\n\nEdited by build.\n")
	if st := f.approvalState(f.worktreeOf("add-login"), "add-login"); st != "stale" {
		t.Fatalf("approval in the worktree is %s", st)
	}
	f.kill("add-login")
	if res := f.pass(); !strings.Contains(res.stdout, "resume add-login") {
		t.Fatalf("stale crash:\n%s", res.stdout)
	}
	f.waitRound("add-login", "build", 2)
	if res := f.tb("history", "add-login"); !strings.Contains(res.stdout, "step_started") {
		t.Fatal(res.stdout)
	}
	f.kill("add-login")
	// A person resumes (a new streak), it crashes, and the owner
	// unapproves the change: no resume.
	if res := f.tb("resume", "add-login"); res.code != 0 {
		t.Fatal(res.stderr)
	}
	f.waitRound("add-login", "build", 3)
	f.kill("add-login")
	if res := f.tb("unapprove", "add-login"); res.code != 0 {
		t.Fatalf("unapprove: %s", res.stderr)
	}
	res := f.pass()
	if strings.Contains(res.stdout, "resume add-login") || !strings.Contains(res.stdout, "approval was withdrawn") {
		t.Errorf("unapproved crash:\n%s", res.stdout)
	}
}

// Scenario: Person stopped a run: no pass resumes or restarts it.
// Scenario: Blocked run: no pass resumes it, and it is reported. A change
// whose lock a person's edit holds is left alone.
func TestAutopilotLeavesAPersonsRuns(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"stopped", "blocked", "edited"}, MaxParallel: 3, RequireApproval: true})
	f.approveAll("stopped", "blocked", "edited")
	f.env.SetPlan(t, map[string]testenv.Action{
		"stopped:build":  {WaitFor: f.gate("never")},
		"edited:build":   {WaitFor: f.gate("never")},
		"blocked:build":  {Tick: []string{"1.1"}},
		"blocked:review": {Status: "blocked", Summary: "review limit reached"},
	})
	f.pass()
	f.waitState("stopped", "running", "build")
	f.waitState("blocked", "blocked", "")
	f.waitRound("edited", "build", 1)
	if res := f.tb("stop", "stopped"); res.code != 0 {
		t.Fatal(res.stderr)
	}
	f.kill("edited")
	// A person's edit holds edited's change lock.
	lock, err := runs.TryChangeLock(mustState(t, f.worktreeOf("edited")).RunLock())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	for i := 0; i < 2; i++ {
		res := f.pass()
		if strings.Contains(res.stdout, "start ") || strings.Contains(res.stdout, "resume ") {
			t.Errorf("pass %d:\n%s", i, res.stdout)
		}
		if !strings.Contains(res.stdout, "blocked needs you: blocked: step review is blocked: review limit reached") {
			t.Errorf("pass %d lacks the blocked report:\n%s", i, res.stdout)
		}
	}
	if s, _ := f.state("stopped"); s != "stopped" {
		t.Errorf("stopped is %s", s)
	}
}
