package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

func (f *cliFixture) changeDir(id string) string {
	return filepath.Join(f.repo, "openspec", "changes", id)
}

func (f *cliFixture) approvalState(dir, id string) approval.State {
	f.t.Helper()
	st, err := approval.Judge(approval.DirOf(dir, id))
	if err != nil {
		f.t.Fatal(err)
	}
	return st.State
}

func (f *cliFixture) mustApprove(id string) {
	f.t.Helper()
	if res := f.tb("approve", id); res.code != 0 {
		f.t.Fatalf("approve %s: %d %s", id, res.code, res.stderr)
	}
}

func (f *cliFixture) commit(msg string) {
	f.t.Helper()
	testenv.Git(f.t, f.repo, "add", "-A")
	testenv.Git(f.t, f.repo, "commit", "-q", "-m", msg)
}

func (f *cliFixture) noWorktree(id string) {
	f.t.Helper()
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.repo), "repo-worktrees", id)); err == nil {
		f.t.Errorf("worktree made for %s", id)
	}
	if out := testenv.Git(f.t, f.repo, "branch", "--list", id); out != "" {
		f.t.Errorf("branch made: %s", out)
	}
}

// Scenario: Approve a valid change. Scenario: Unapprove.
func TestApproveAndUnapprove(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	sidecar := filepath.Join(f.changeDir("add-login"), "tuckerbox.toml")
	testenv.WriteFile(t, sidecar, "# needs users first\ndepends_on = [\"add-users\"]\n")
	f.commit("depends_on")
	head := testenv.Git(t, f.repo, "rev-parse", "HEAD")

	res := f.tb("approve", "add-login")
	if res.code != 0 {
		t.Fatalf("approve: %d %s", res.code, res.stderr)
	}
	data, _ := os.ReadFile(sidecar)
	if !strings.HasPrefix(string(data), "# needs users first\ndepends_on = [\"add-users\"]\n") || !strings.Contains(string(data), "[approval]") ||
		!strings.Contains(string(data), `by = "Test"`) {
		t.Errorf("tuckerbox.toml:\n%s", data)
	}
	if !strings.Contains(res.stdout, "not committed") || !strings.Contains(res.stdout, f.repo) {
		t.Errorf("approve output: %s", res.stdout)
	}
	if f.approvalState(f.repo, "add-login") != approval.Approved {
		t.Error("not approved")
	}
	if got := testenv.Git(t, f.repo, "rev-parse", "HEAD"); got != head {
		t.Error("approve committed")
	}

	res = f.tb("unapprove", "add-login")
	if res.code != 0 || !strings.Contains(res.stdout, "not committed") {
		t.Fatalf("unapprove: %d %s %s", res.code, res.stdout, res.stderr)
	}
	if f.approvalState(f.repo, "add-login") != approval.None {
		t.Error("not none after unapprove")
	}
	data, _ = os.ReadFile(sidecar)
	if string(data) != "# needs users first\ndepends_on = [\"add-users\"]\n" {
		t.Errorf("after unapprove:\n%q", data)
	}
	// Unapproving again changes nothing and exits 0.
	if res := f.tb("unapprove", "add-login"); res.code != 0 || !strings.Contains(res.stdout, "nothing changed") {
		t.Errorf("second unapprove: %d %s %s", res.code, res.stdout, res.stderr)
	}
	if out := testenv.Git(t, f.repo, "status", "--porcelain"); out != "" {
		t.Errorf("files left changed: %s", out)
	}
	if res := f.tb("approve", "no-such-change"); res.code == 0 {
		t.Error("approved a change that doesn't exist")
	}
}

// Scenario: Approve an invalid change.
func TestApproveInvalidChange(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	sidecar := filepath.Join(f.changeDir("add-login"), "tuckerbox.toml")
	testenv.WriteFile(t, sidecar, "depends_on = []\n")
	testenv.WriteFile(t, filepath.Join(f.changeDir("add-login"), "INVALID"), "Requirement must have a scenario\n")
	res := f.tb("approve", "add-login")
	if res.code == 0 || !strings.Contains(res.stderr, "Requirement must have a scenario") {
		t.Errorf("approve: %d %s", res.code, res.stderr)
	}
	if data, _ := os.ReadFile(sidecar); string(data) != "depends_on = []\n" {
		t.Errorf("tuckerbox.toml changed:\n%s", data)
	}
}

// Approve refuses while a run is running on the change, naming its step,
// and holds the lock (change-edits 3.6, for approve and unapprove).
func TestApproveDuringRun(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: f.gate("build")}})
	if res := f.tb("run", "add-login"); res.code != 0 {
		t.Fatalf("run: %d %s", res.code, res.stderr)
	}
	f.waitState("add-login", "running", "build")
	wt := filepath.Join(filepath.Dir(f.repo), "repo-worktrees", "add-login")
	for _, cmd := range []string{"approve", "unapprove"} {
		res := f.tb(cmd, "add-login")
		if res.code == 0 || !strings.Contains(res.stderr, "running (step build)") {
			t.Errorf("%s during build: %d %s", cmd, res.code, res.stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(wt, "openspec", "changes", "add-login", "tuckerbox.toml")); err == nil {
		t.Error("tuckerbox.toml written during the run")
	}
	f.open("build")
	f.waitState("add-login", "ready", "")
	// Once the run stops, approve acts in the worktree, not the checkout.
	res := f.tb("approve", "add-login")
	if res.code != 0 || !strings.Contains(res.stdout, wt) {
		t.Fatalf("approve after run: %d %s %s", res.code, res.stdout, res.stderr)
	}
	if f.approvalState(wt, "add-login") != approval.Approved || f.approvalState(f.repo, "add-login") != approval.None {
		t.Error("approval not written in the worktree only")
	}
}

// Scenario: Approval not committed.
func TestRunGateApprovalNotCommitted(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	res := f.tb("run", "add-login")
	if res.code == 0 || !strings.Contains(res.stderr, "base branch main has no approval") || strings.Contains(res.stderr, "checkout") {
		t.Errorf("unapproved run: %d %s", res.code, res.stderr)
	}
	f.mustApprove("add-login")
	res = f.tb("run", "add-login")
	if res.code == 0 || !strings.Contains(res.stderr, "base branch main has no approval") ||
		!strings.Contains(res.stderr, "the checkout "+f.repo+" has an approval") {
		t.Errorf("uncommitted approval: %d %s", res.code, res.stderr)
	}
	f.noWorktree("add-login")
	if len(f.status().Runs) != 0 {
		t.Error("run recorded")
	}
	f.commit("approve add-login")
	if res := f.tb("run", "--foreground", "add-login"); res.code != 0 {
		t.Fatalf("committed approval: %d %s", res.code, res.stderr)
	}
}

// Scenario: Stale approval, on the base branch and in a worktree.
func TestRunGateStaleApproval(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login", "add-users"}, RequireApproval: true})
	f.mustApprove("add-login")
	f.mustApprove("add-users")
	f.commit("approve")
	testenv.WriteFile(t, filepath.Join(f.changeDir("add-login"), "design.md"), "# Design\n\nEdited.\n")
	f.commit("edit design")
	res := f.tb("run", "add-login")
	if res.code == 0 || !strings.Contains(res.stderr, "approval is stale on the base branch main") {
		t.Errorf("stale run: %d %s", res.code, res.stderr)
	}
	f.noWorktree("add-login")

	// A change with a worktree is judged there.
	f.env.SetPlan(t, map[string]testenv.Action{"add-users:review": {Status: "blocked", Summary: "needs a person"}})
	if res := f.tb("run", "--foreground", "add-users"); res.code != 0 {
		t.Fatalf("run add-users: %d %s", res.code, res.stderr)
	}
	wt := filepath.Join(filepath.Dir(f.repo), "repo-worktrees", "add-users")
	testenv.WriteFile(t, filepath.Join(wt, "openspec", "changes", "add-users", "design.md"), "# Design\n\nIn the worktree.\n")
	res = f.tb("run", "add-users")
	if res.code == 0 || !strings.Contains(res.stderr, "approval is stale in its worktree "+wt) {
		t.Errorf("stale in worktree: %d %s", res.code, res.stderr)
	}
	// Ticking a task keeps the approval.
	tasks := filepath.Join(f.changeDir("add-users"), "tasks.md")
	data, _ := os.ReadFile(tasks)
	testenv.WriteFile(t, tasks, strings.Replace(string(data), "- [ ] 1.1", "- [x] 1.1", 1))
	if f.approvalState(f.repo, "add-users") != approval.Approved {
		t.Error("ticking a task voided the approval")
	}
}

// Scenario: Run all with unapproved changes.
func TestRunAllSkipsUnapproved(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a", "b", "c"}, MaxParallel: 2, RequireApproval: true})
	f.mustApprove("a")
	f.mustApprove("b")
	f.commit("approve a and b")
	res := f.tb("run", "--all")
	if res.code != 0 {
		t.Fatalf("run --all: %d %s %s", res.code, res.stdout, res.stderr)
	}
	for _, c := range []string{"a", "b"} {
		if !strings.Contains(res.stdout, c+": started") {
			t.Errorf("%s not started:\n%s", c, res.stdout)
		}
	}
	if !strings.Contains(res.stdout, "c: skipped, not approved (none)") {
		t.Errorf("c not listed as skipped:\n%s", res.stdout)
	}
	f.waitState("a", "ready", "")
	f.waitState("b", "ready", "")
	if s, _ := f.state("c"); s != "" {
		t.Errorf("c has a run: %s", s)
	}
	f.noWorktree("c")
}

// Scenario: Approval not required.
func TestRunWithoutApprovalWhenNotRequired(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	if res := f.tb("run", "--foreground", "add-login"); res.code != 0 {
		t.Fatalf("run: %d %s", res.code, res.stderr)
	}
}

// Scenario: Machine config sets the key.
func TestCheckRefusesMachineRequireApproval(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{})
	testenv.WriteFile(t, filepath.Join(f.env.Home, "config", "tuckerbox", "config.toml"), "require_approval = false\n")
	res := f.tb("check")
	if res.code == 0 || !strings.Contains(res.stderr, "require_approval") || !strings.Contains(res.stderr, "belongs in repository config") {
		t.Errorf("check: %d %s", res.code, res.stderr)
	}
}

// Review finding 1: a change whose branch exists without a worktree is
// judged on that branch, the one its worktree is made from, not the base.
func TestRunGateRetainedBranch(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	testenv.Git(t, f.repo, "branch", "add-login") // made before any approval
	f.mustApprove("add-login")
	f.commit("approve add-login on main")
	for _, args := range [][]string{
		{"run", "--foreground", "add-login"},
		{"run", "add-login"},
		{"engine", "--mode", "start", "add-login"}, // the engine refuses by itself
	} {
		res := f.tb(args...)
		if res.code == 0 || !strings.Contains(res.stderr, "its branch add-login has no approval") {
			t.Errorf("%v: %d %s", args, res.code, res.stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.repo), "repo-worktrees", "add-login")); err == nil {
		t.Error("worktree made")
	}
	if len(f.status().Runs) != 0 {
		t.Error("run recorded")
	}
	// With the approval on the branch, it starts from the branch.
	testenv.Git(t, f.repo, "branch", "-f", "add-login", "main")
	if res := f.tb("run", "--foreground", "add-login"); res.code != 0 {
		t.Fatalf("approved branch: %d %s", res.code, res.stderr)
	}
}

// Review finding 2: approval is judged again by the engine under the change
// lock. An unapprove that slips in after tuckerbox run's early check (here:
// before the engine run starts) stops the run, and leaves its state as it was.
func TestRunGateUnderLockAfterUnapprove(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a"}, RequireApproval: true})
	f.mustApprove("a")
	f.commit("approve a")
	f.env.SetPlan(t, map[string]testenv.Action{"a:review": {Status: "blocked", Summary: "needs a person"}})
	if res := f.tb("run", "--foreground", "a"); res.code != 0 {
		t.Fatalf("first run: %d %s", res.code, res.stderr)
	}
	if s, _ := f.state("a"); s != "blocked" {
		t.Fatalf("a is %s", s)
	}
	// tuckerbox run a: its early check passes, then unapprove runs, then
	// the engine it spawns starts.
	if res := f.tb("unapprove", "a"); res.code != 0 {
		t.Fatalf("unapprove: %d %s", res.code, res.stderr)
	}
	res := f.tb("engine", "--mode", "start", "a")
	wt := filepath.Join(filepath.Dir(f.repo), "repo-worktrees", "a")
	if res.code == 0 || !strings.Contains(res.stderr, "no approval in its worktree "+wt) {
		t.Errorf("engine after unapprove: %d %s", res.code, res.stderr)
	}
	if s, _ := f.state("a"); s != "blocked" {
		t.Errorf("refused start changed the run: %s", s)
	}
	if got := len(f.env.Starts(t)); got != 2 {
		t.Errorf("agent started again: %d starts", got)
	}
}

// Review finding 2, base branch: the engine makes the worktree at the
// commit tuckerbox run checked, even when the base moved on to text that
// is not approved; without that commit it judges the base as it is now.
func TestRunGatePinsCheckedCommit(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a"}, RequireApproval: true})
	f.mustApprove("a")
	f.commit("approve a")
	checked := testenv.Git(t, f.repo, "rev-parse", "HEAD")
	testenv.WriteFile(t, filepath.Join(f.changeDir("a"), "design.md"), "# Design\n\nNot approved.\n")
	f.commit("edit a after approval")

	res := f.tb("engine", "--mode", "start", "a")
	if res.code == 0 || !strings.Contains(res.stderr, "approval is stale on the base branch main") {
		t.Errorf("engine without the checked commit: %d %s", res.code, res.stderr)
	}
	f.noWorktree("a")

	if res := f.tb("engine", "--mode", "start", "--start-rev", checked, "a"); res.code != 0 {
		t.Fatalf("engine at the checked commit: %d %s %s", res.code, res.stdout, res.stderr)
	}
	wt := filepath.Join(filepath.Dir(f.repo), "repo-worktrees", "a")
	if _, err := os.Stat(filepath.Join(wt, "openspec", "changes", "a", "design.md")); err == nil {
		t.Error("worktree has the unapproved design: not made at the checked commit")
	}
}
