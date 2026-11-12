package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

const scopeTasks = "# Tasks\n\n" +
	"## 1. First\n\n" +
	"- [ ] 1.1 One\n" +
	"- [ ] 1.2 Two\n" +
	"- [ ] 1.3 Three\n" +
	"- [ ] 1.4 Four\n\n" +
	"## 2. Second\n\n" +
	"- [ ] 2.1 Five\n" +
	"- [ ] 2.2 Six\n" +
	"- [ ] 2.3 Seven\n\n" +
	"## 6. Wrap-up\n\n" +
	"- [ ] 6.1 README\n" +
	"- [ ] 6.3 (ship) Archive this change\n"

func (f *cliFixture) tasks(id, text string) {
	f.t.Helper()
	testenv.WriteFile(f.t, filepath.Join(f.changeDir(id), "tasks.md"), text)
	f.commit("tasks of " + id)
}

func (f *cliFixture) worktreeOf(id string) string {
	return filepath.Join(filepath.Dir(f.repo), "repo-worktrees", id)
}

// runOf reads id's run.json in its worktree.
func (f *cliFixture) runOf(id string) *runs.Run {
	f.t.Helper()
	st, err := statedir.Resolve(context.Background(), f.worktreeOf(id))
	if err != nil {
		f.t.Fatal(err)
	}
	r, err := runs.ReadRun(st.RunFile())
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

func scopeString(r *runs.Run) string {
	if r.Scope == nil {
		return "<none>"
	}
	return r.Scope.Spec + " " + strings.Join(r.Scope.Tasks, ",")
}

func (f *cliFixture) refused(res cmdResult, msg string) {
	f.t.Helper()
	if res.code == 0 || !strings.Contains(res.stderr, msg) {
		f.t.Errorf("exit %d, stderr %q; want a refusal saying %q", res.code, res.stderr, msg)
	}
}

// Scenario: Unknown task. Scenario: Scope already done. Scenario: Scope
// with run all. Scenario: Scope of a ship task. Each refuses before any
// worktree or run is made.
func TestRunScopeRefusals(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login", "add-users"}})
	f.tasks("add-login", strings.NewReplacer("- [ ] 1.", "- [x] 1.").Replace(scopeTasks))
	f.refused(f.tb("run", "add-login", "--tasks", "7"), "tasks.md has no group 7")
	f.refused(f.tb("run", "add-login", "--tasks", "2.9"), "tasks.md has no task 2.9")
	f.refused(f.tb("run", "add-login", "--tasks", "2.3-2.1"), "the range ends before it starts")
	f.refused(f.tb("run", "add-login", "--tasks", "1"), "the tasks in scope 1 are all ticked")
	f.refused(f.tb("run", "add-login", "--tasks", "6.3"), "6.3 is a ship task: it is done by shipping")
	f.refused(f.tb("run", "add-login", "--tasks", "two"), "use a group (2), a task (2.3)")
	for _, args := range [][]string{
		{"run", "--all", "--tasks", "1"},
		{"run", "add-login", "add-users", "--tasks", "2"},
	} {
		res := f.tb(args...)
		f.refused(res, "--tasks takes one change")
	}
	f.noWorktree("add-login")
	f.noWorktree("add-users")
	if n := len(f.status().Runs); n != 0 {
		t.Errorf("%d runs recorded", n)
	}
}

// Scenario: Run one group. Scenario: Range across groups. Scenario: Next
// group (run.json keeps 2, not next).
func TestRunScopes(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"one", "range", "next"}})
	f.tasks("one", scopeTasks)
	f.tasks("range", scopeTasks)
	f.tasks("next", strings.NewReplacer("- [ ] 1.", "- [x] 1.").Replace(scopeTasks))
	f.env.SetPlan(t, map[string]testenv.Action{
		"one:build":   {Tick: []string{"2.1", "2.2", "2.3"}},
		"range:build": {Tick: []string{"1.3", "1.4", "2.1"}},
		"next:build":  {Tick: []string{"2.1", "2.2", "2.3"}},
	})
	for _, c := range []struct{ id, spec, want string }{
		{"one", "2", "2 2.1,2.2,2.3"},
		{"range", "1.3-2.1", "1.3-2.1 1.3,1.4,2.1"},
		{"next", "next", "2 2.1,2.2,2.3"},
	} {
		if res := f.tb("run", "--foreground", c.id, "--tasks", c.spec); res.code != 0 {
			t.Fatalf("run %s --tasks %s: %d %s", c.id, c.spec, res.code, res.stderr)
		}
		r := f.runOf(c.id)
		if r.State != runs.Ready || scopeString(r) != c.want {
			t.Errorf("%s: %s scope %s, want %s (%s)", c.id, r.State, scopeString(r), c.want, r.Reason)
		}
	}
	// In the background, too: the engine gets the resolved scope.
	f.env.SetPlan(t, map[string]testenv.Action{"next:build": {Tick: []string{"6.1"}}})
	if res := f.tb("run", "next", "--tasks", "next"); res.code != 0 {
		t.Fatalf("run next --tasks next: %d %s", res.code, res.stderr)
	}
	f.waitState("next", "ready", "")
	if r := f.runOf("next"); scopeString(r) != "6 6.1" || !r.Complete {
		t.Errorf("next group 6: scope %s complete %v", scopeString(r), r.Complete)
	}
}

// Scenario: Second scope while running: refused, pointing at the active
// run.
func TestSecondScopeWhileRunning(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.tasks("add-login", scopeTasks)
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: f.gate("build")}})
	if res := f.tb("run", "add-login", "--tasks", "1"); res.code != 0 {
		t.Fatalf("run: %d %s", res.code, res.stderr)
	}
	f.waitState("add-login", "running", "build")
	f.refused(f.tb("run", "add-login", "--tasks", "2"), "already has an active run")
	f.open("build")
	f.waitState("add-login", "blocked", "")
}

// Scenario: Ticks keep the next scope approved. Scenario: Design edited
// during a scope: the next scope is refused as stale and the run stays
// ready.
func TestNextScopeIsGated(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"ticks", "edits"}, RequireApproval: true})
	f.tasks("ticks", scopeTasks)
	f.tasks("edits", scopeTasks)
	f.mustApprove("ticks")
	f.mustApprove("edits")
	f.commit("approve")
	f.env.SetPlan(t, map[string]testenv.Action{
		"build:1":       {Tick: []string{"1.1", "1.2", "1.3", "1.4"}},
		"edits:build:1": {Tick: []string{"1.1", "1.2", "1.3", "1.4"}, Edit: "design.md"},
		"build:2":       {Tick: []string{"2.1", "2.2", "2.3"}},
	})
	for _, id := range []string{"ticks", "edits"} {
		if res := f.tb("run", "--foreground", id, "--tasks", "1"); res.code != 0 {
			t.Fatalf("%s scope 1: %d %s", id, res.code, res.stderr)
		}
	}
	if res := f.tb("run", "--foreground", "ticks", "--tasks", "2"); res.code != 0 {
		t.Fatalf("ticks scope 2: %d %s", res.code, res.stderr)
	}
	if r := f.runOf("ticks"); r.State != runs.Ready || scopeString(r) != "2 2.1,2.2,2.3" || r.Round != 2 {
		t.Errorf("ticks: %s scope %s round %d", r.State, scopeString(r), r.Round)
	}
	f.refused(f.tb("run", "edits", "--tasks", "2"), "approval is stale in its worktree")
	if r := f.runOf("edits"); r.State != runs.Ready || scopeString(r) != "1 1.1,1.2,1.3,1.4" {
		t.Errorf("edits after refusal: %s scope %s", r.State, scopeString(r))
	}
}
