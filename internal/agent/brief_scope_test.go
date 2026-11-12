package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/scope"
)

func writeTasks(t *testing.T, root, change, text string) {
	t.Helper()
	p := filepath.Join(root, "openspec", "changes", change, "tasks.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

const briefTasks = "# Tasks\n\n## 2. Scope\n\n" +
	"- [ ] 2.1 Parse --tasks\n" +
	"- [ ] 2.2 Refuse unknown numbers\n" +
	"- [ ] 2.3 Out of scope\n\n" +
	"## 3. Wrap-up\n\n" +
	"- [ ] 3.1 (ship) Archive this change\n"

// Scenario: Brief for a scope. The brief lists the scope's tasks with
// their text and the in-scope rules, and says not to tick ship tasks.
func TestBriefForAScope(t *testing.T) {
	root := t.TempDir()
	writeTasks(t, root, "add-login", briefTasks)
	b := Brief{Change: "add-login", Step: "review", Role: "reviewer", Round: 3, Root: root, ResultPath: "r.json",
		Scope: &scope.Scope{Spec: "2.1-2.2", Tasks: []string{"2.1", "2.2", "2.9"}}}
	text, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Scope\n\nThis run covers only these tasks:\n- 2.1 Parse --tasks\n- 2.2 Refuse unknown numbers\n- 2.9 (no longer in tasks.md)\n",
		"Work on, tick and judge only these tasks.",
		"Tasks outside the scope are not part of this run: don't do them, don't tick them, and don't reject the change because they are undone.",
		"Work or ticks outside the scope are a finding.",
		"Tasks whose text starts with `(ship)` are done by shipping the change, not by a step: don't tick them.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("brief lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "2.3 Out of scope") {
		t.Error("brief lists a task outside the scope")
	}
	// Without a scope there is no Scope section.
	b.Scope = nil
	text, _ = b.Render()
	if strings.Contains(text, "## Scope") {
		t.Error("unscoped brief has a Scope section")
	}
}

// Scenario: Build after rejection, with a scope set: the brief still
// includes round 1's findings, and the scope.
func TestBuildAfterRejectionWithScope(t *testing.T) {
	review := newStep(t, "review", "reviewer", 1, fake("rejected"))
	writeTasks(t, review.Worktree, review.Change, briefTasks)
	review.Scope = &scope.Scope{Spec: "2", Tasks: []string{"2.1", "2.2"}}
	o := run(t, review, fast)
	if o.Status() != Rejected || o.Result == nil {
		t.Fatalf("review outcome %+v", o)
	}
	build := review
	build.Step, build.Role, build.Round, build.Agent = "build", "builder", 2, fake("done")
	build.Earlier = []Result{*o.Result}
	bo := run(t, build, fast)
	if bo.Status() != Done {
		t.Fatalf("build: %s", bo.FailureReason())
	}
	brief := readFile(t, bo.BriefPath)
	for _, want := range []string{
		"## Findings from earlier rounds",
		"### review, round 1: rejected",
		"[high] `login.go`: password is logged",
		"## Scope",
		"- 2.1 Parse --tasks",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief lacks %q:\n%s", want, brief)
		}
	}
}
