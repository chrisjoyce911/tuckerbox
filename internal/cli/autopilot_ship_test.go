package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// setShip commits the repository config with ship mode m (and extra).
func (f *cliFixture) setShip(m, extra string) {
	f.t.Helper()
	p := filepath.Join(f.repo, ".tuckerbox", "config.toml")
	data, err := os.ReadFile(p)
	if err != nil {
		f.t.Fatal(err)
	}
	// A shipping remote: fetched from a local bare repository, pushed to
	// a forge URL (nothing here ever pushes). The base stays local main.
	bare := filepath.Join(f.t.TempDir(), "origin.git")
	testenv.Git(f.t, f.repo, "init", "-q", "--bare", bare)
	testenv.Git(f.t, f.repo, "remote", "add", "origin", bare)
	testenv.Git(f.t, f.repo, "remote", "set-url", "--push", "origin", "https://github.com/example/app.git")
	testenv.WriteFile(f.t, p, strings.Replace(string(data), `ship = "local"`, `ship = "`+m+`"`, 1)+extra+"\n[worktree]\nbase = \"main\"\n")
	f.commit("ship = " + m)
}

const threeGroups = "# Tasks\n\n## 1. One\n\n- [ ] 1.1 a\n\n## 2. Two\n\n- [ ] 2.1 b\n\n## 3. Three\n\n- [ ] 3.1 c\n- [ ] 3.2 d\n- [ ] 3.3 (ship) Archive this change\n"

// Scenario: First group in merge mode: the run is ready, nothing is
// archived or pushed, and the next pass starts group 2.
func TestAutopilotFirstGroupInMergeMode(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	f.tasks("add-login", threeGroups)
	f.setShip("merge", "")
	f.approveAll("add-login")
	f.env.SetPlan(t, map[string]testenv.Action{"build:1": {Tick: []string{"1.1"}}, "build:2": {Tick: []string{"2.1"}}})
	f.pass()
	f.waitState("add-login", "ready", "")
	if r := f.runOf("add-login"); r.Complete || r.Scope.Spec != "1" {
		t.Fatalf("group 1: %+v %v", r.Scope, r.Complete)
	}
	if _, err := os.Stat(filepath.Join(f.worktreeOf("add-login"), "openspec", "changes", "archive")); err == nil {
		t.Error("archived")
	}
	if res := f.pass(); !strings.Contains(res.stdout, "start add-login --tasks next") {
		t.Errorf("pass 2:\n%s", res.stdout)
	}
	waitFor(t, "group 2 ready", func() bool { r := f.runOf("add-login"); return r.Scope.Spec == "2" && r.State == runs.Ready })
}

// Scenario: Whole change left incomplete (merge mode): blocked, reported,
// never restarted. Scenario: Whole change left ready in local mode: ready
// and not complete; reported, and no pass requests a start.
func TestAutopilotWholeChangeLeftIncomplete(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true, Extra: "\n[autopilot]\nscope = \"change\"\n"})
	f.tasks("add-login", threeGroups)
	f.setShip("merge", "")
	f.approveAll("add-login")
	all := testenv.Action{Tick: []string{"1.1", "2.1", "3.1"}}
	f.env.SetPlan(t, map[string]testenv.Action{"build": all})
	f.pass()
	f.waitState("add-login", "blocked", "")
	for i := 0; i < 3; i++ {
		res := f.pass()
		if strings.Contains(res.stdout, "start ") || !strings.Contains(res.stdout, "add-login needs you: blocked: tasks not ticked: 3.2") {
			t.Fatalf("merge pass %d:\n%s", i, res.stdout)
		}
	}

	g := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true, Extra: "\n[autopilot]\nscope = \"change\"\n"})
	g.tasks("add-login", threeGroups)
	g.approveAll("add-login")
	g.env.SetPlan(t, map[string]testenv.Action{"build": all})
	g.pass()
	g.waitState("add-login", "ready", "")
	for i := 0; i < 3; i++ {
		res := g.pass()
		if strings.Contains(res.stdout, "start ") || !strings.Contains(res.stdout, "add-login needs you: ready but not complete (tasks not ticked: 3.2)") {
			t.Fatalf("local pass %d:\n%s", i, res.stdout)
		}
	}
	if r := g.runOf("add-login"); r.Round != 1 || r.State != runs.Ready {
		t.Errorf("run moved: %s round %d", r.State, r.Round)
	}
}

// Scenario: Group mode picks up the rest: a run someone started without
// a scope is ready with group 3 unticked; the pass starts it with --tasks
// next, which resolves to 3. Then, with the last group complete and the
// change archived on the base, later passes start nothing for it.
func TestAutopilotGroupModePicksUpTheRest(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	f.tasks("add-login", threeGroups)
	f.approveAll("add-login")
	f.env.SetPlan(t, map[string]testenv.Action{"build:1": {Tick: []string{"1.1", "2.1"}}, "build:2": {Tick: []string{"3.1", "3.2"}}})
	if res := f.tb("run", "--foreground", "add-login"); res.code != 0 {
		t.Fatal(res.stderr)
	}
	if res := f.pass(); !strings.Contains(res.stdout, "start add-login --tasks next") {
		t.Fatalf("pass:\n%s", res.stdout)
	}
	waitFor(t, "group 3 ready", func() bool {
		r := f.runOf("add-login")
		return r.Scope != nil && r.Scope.Spec == "3" && r.State == runs.Ready
	})
	if r := f.runOf("add-login"); !r.Complete {
		t.Error("not complete with only the ship task left")
	}
	if res := f.pass(); strings.TrimSpace(res.stdout) != "" {
		t.Errorf("complete change got:\n%s", res.stdout)
	}
	// Archived on the base: finished.
	if err := os.MkdirAll(filepath.Join(f.repo, "openspec", "changes", "archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	testenv.Git(t, f.repo, "mv", "openspec/changes/add-login", "openspec/changes/archive/2026-11-12-add-login")
	f.commit("archive add-login")
	if res := f.pass(); strings.TrimSpace(res.stdout) != "" {
		t.Errorf("archived change got:\n%s", res.stdout)
	}
}
