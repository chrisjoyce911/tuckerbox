package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/priority"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

func (f *cliFixture) sidecar(dir, id string) string {
	return filepath.Join(dir, "openspec", "changes", id, "tuckerbox.toml")
}

func (f *cliFixture) readSidecar(dir, id string) string {
	f.t.Helper()
	data, err := os.ReadFile(f.sidecar(dir, id))
	if err != nil && !os.IsNotExist(err) {
		f.t.Fatal(err)
	}
	return string(data)
}

func (f *cliFixture) order() []string {
	f.t.Helper()
	cs, err := priority.Load(f.repo)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for _, c := range priority.Sort(cs) {
		out = append(out, c.ID)
	}
	return out
}

// Scenario: List. B at 1000 and A unranked: B at position 1 with 1000,
// then A at position 2 with -.
func TestPriorityList(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a", "b"}})
	testenv.WriteFile(t, f.sidecar(f.repo, "b"), "priority = 1000\n")
	res := f.tb("priority")
	if res.code != 0 {
		t.Fatalf("priority: %d %s", res.code, res.stderr)
	}
	lines := strings.Split(strings.TrimSpace(res.stdout), "\n")
	if len(lines) != 3 || strings.Join(strings.Fields(lines[1]), " ") != "1 1000 b" || strings.Join(strings.Fields(lines[2]), " ") != "2 - a" {
		t.Errorf("list:\n%s", res.stdout)
	}
	res = f.tb("priority", "--json")
	var doc priorityDoc
	if err := json.Unmarshal([]byte(res.stdout), &doc); err != nil || res.code != 0 {
		t.Fatalf("--json: %v %s", err, res.stdout)
	}
	if doc.Version != 1 || len(doc.Changes) != 2 || doc.Changes[0].Change != "b" || *doc.Changes[0].Priority != 1000 ||
		doc.Changes[1].Position != 2 || doc.Changes[1].Priority != nil {
		t.Errorf("json: %s", res.stdout)
	}
	if !strings.Contains(res.stdout, `"priority": null`) {
		t.Errorf("unranked should be null: %s", res.stdout)
	}
	for _, args := range [][]string{{"priority", "a"}, {"priority", "a", "1", "x"}, {"priority", "a", "1", "--json"}, {"priority", "a", "zero"}} {
		if res := f.tb(args...); res.code != 2 {
			t.Errorf("%v: %d %s", args, res.code, res.stderr)
		}
	}
}

// Scenario: Move to a position (A, B, C, D; D to 2 gives A, D, B, C), Up
// at the top (nothing changes, exit 0) and Unknown change (non-zero, names
// it, writes nothing). Other keys, tables and comments are kept, and
// nothing is committed.
func TestPriorityMove(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a", "b", "c", "d"}})
	head := testenv.Git(t, f.repo, "rev-parse", "HEAD")
	d := "# D waits for A\ndepends_on = [\"a\"]\n\n[approval]\nhash = \"sha256:x\"\nby = \"T\"\nat = 2026-11-12T10:00:00+10:00\n"
	testenv.WriteFile(t, f.sidecar(f.repo, "d"), d)

	res := f.tb("priority", "d", "2")
	if res.code != 0 {
		t.Fatalf("priority d 2: %d %s", res.code, res.stderr)
	}
	if got := strings.Join(f.order(), ","); got != "a,d,b,c" {
		t.Errorf("order %s", got)
	}
	if !strings.Contains(res.stdout, "not committed") || !strings.Contains(res.stdout, "from position 4 to 2") {
		t.Errorf("output: %s", res.stdout)
	}
	if got := f.readSidecar(f.repo, "d"); got != "# D waits for A\ndepends_on = [\"a\"]\npriority = 2000\n\n[approval]\nhash = \"sha256:x\"\nby = \"T\"\nat = 2026-11-12T10:00:00+10:00\n" {
		t.Errorf("d's file:\n%s", got)
	}
	if testenv.Git(t, f.repo, "rev-parse", "HEAD") != head {
		t.Error("priority committed")
	}
	if f.readSidecar(f.repo, "b") != "" || f.readSidecar(f.repo, "c") != "" {
		t.Error("b or c written")
	}

	before := testenv.Git(t, f.repo, "status", "--porcelain")
	res = f.tb("priority", "a", "up")
	if res.code != 0 || !strings.Contains(res.stdout, "nothing changed") {
		t.Errorf("up at the top: %d %s %s", res.code, res.stdout, res.stderr)
	}
	res = f.tb("priority", "add-nothing", "top")
	if res.code == 0 || !strings.Contains(res.stderr, "add-nothing") {
		t.Errorf("unknown change: %d %s", res.code, res.stderr)
	}
	if after := testenv.Git(t, f.repo, "status", "--porcelain"); after != before {
		t.Errorf("files changed:\n%s\nwas\n%s", after, before)
	}

	// down and top, and a position past the end means last.
	if res := f.tb("priority", "a", "down"); res.code != 0 {
		t.Fatal(res.stderr)
	}
	if got := strings.Join(f.order(), ","); got != "d,a,b,c" {
		t.Errorf("after down: %s", got)
	}
	if res := f.tb("priority", "d", "99"); res.code != 0 {
		t.Fatal(res.stderr)
	}
	if got := strings.Join(f.order(), ","); got != "a,b,c,d" {
		t.Errorf("after 99: %s", got)
	}
	if res := f.tb("priority", "c", "top"); res.code != 0 {
		t.Fatal(res.stderr)
	}
	if got := strings.Join(f.order(), ","); got != "c,a,b,d" {
		t.Errorf("after top: %s", got)
	}
}

// An archived change, and an id that isn't one, are refused.
func TestPriorityRefusesArchived(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a"}})
	testenv.WriteFile(t, filepath.Join(f.repo, "openspec", "changes", "archive", "2026-01-01-old", "proposal.md"), "# old\n")
	if res := f.tb("priority", "old", "top"); res.code == 0 || !strings.Contains(res.stderr, "archived") {
		t.Errorf("archived: %d %s", res.code, res.stderr)
	}
	if res := f.tb("priority", "../a", "top"); res.code == 0 {
		t.Errorf("bad id accepted")
	}
}

// Scenario: Reorder a queued change. add-login is queued: priority writes
// the checkout's tuckerbox.toml, not its worktree's, and says it is not
// committed. Run from inside the worktree, it still writes the checkout.
func TestReorderAQueuedChange(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a", "add-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"a:build": {WaitFor: f.gate("a")}})
	if res := f.tb("run", "a"); res.code != 0 {
		t.Fatal(res.stderr)
	}
	f.waitState("a", "running", "build")
	if res := f.tb("run", "add-login"); res.code != 0 {
		t.Fatal(res.stderr)
	}
	f.waitState("add-login", "queued", "")
	wt := filepath.Join(filepath.Dir(f.repo), "repo-worktrees", "add-login")
	wtBefore := testenv.Git(t, wt, "status", "--porcelain")

	res := f.tb("priority", "add-login", "top")
	if res.code != 0 || !strings.Contains(res.stdout, "not committed") {
		t.Fatalf("priority: %d %s %s", res.code, res.stdout, res.stderr)
	}
	if !strings.Contains(f.readSidecar(f.repo, "add-login"), "priority = 1000") {
		t.Errorf("checkout file: %q", f.readSidecar(f.repo, "add-login"))
	}
	if f.readSidecar(wt, "add-login") != "" || testenv.Git(t, wt, "status", "--porcelain") != wtBefore {
		t.Error("the worktree was touched")
	}
	if s, _ := f.state("add-login"); s != "queued" {
		t.Errorf("add-login is %s", s)
	}
	res = tbIn(t, wt, "priority", "a", "top")
	if res.code != 0 || !strings.Contains(res.stdout, f.repo) {
		t.Fatalf("from the worktree: %d %s %s", res.code, res.stdout, res.stderr)
	}
	if f.readSidecar(wt, "a") != "" || !strings.Contains(f.readSidecar(f.repo, "a"), "priority = 500") {
		t.Errorf("from the worktree: checkout %q, worktree %q", f.readSidecar(f.repo, "a"), f.readSidecar(wt, "a"))
	}
}

// Scenario: Reorder an approved change. The move, committed and pulled on
// a second machine, leaves the approval approved on both; the approval
// hash is the same with and without the priority line, and approve and
// unapprove keep it.
func TestReorderAnApprovedChange(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a", "add-login"}, RequireApproval: true})
	f.mustApprove("add-login")
	f.commit("approve")
	hashBefore, err := approval.Hash(approval.DirOf(f.repo, "add-login"))
	if err != nil {
		t.Fatal(err)
	}
	if res := f.tb("priority", "add-login", "top"); res.code != 0 {
		t.Fatal(res.stderr)
	}
	if h, _ := approval.Hash(approval.DirOf(f.repo, "add-login")); h != hashBefore {
		t.Error("the priority line changed the approval hash")
	}
	f.commit("priority")
	other := filepath.Join(t.TempDir(), "other")
	testenv.Git(t, filepath.Dir(other), "clone", "-q", f.repo, other)
	for _, dir := range []string{f.repo, other} {
		if st := f.approvalState(dir, "add-login"); st != approval.Approved {
			t.Errorf("%s: approval %s", dir, st)
		}
		if !strings.Contains(f.readSidecar(dir, "add-login"), "priority = 1000") {
			t.Errorf("%s: no priority", dir)
		}
	}
	// approve and unapprove keep priority.
	if res := f.tb("unapprove", "add-login"); res.code != 0 {
		t.Fatal(res.stderr)
	}
	if got := f.readSidecar(f.repo, "add-login"); got != "priority = 1000\n" {
		t.Errorf("after unapprove: %q", got)
	}
	f.mustApprove("add-login")
	if got := f.readSidecar(f.repo, "add-login"); !strings.HasPrefix(got, "priority = 1000\n") || !strings.Contains(got, "[approval]") {
		t.Errorf("after approve: %q", got)
	}
}

// Scenario: Priority and approve at once. Both run as separate processes
// on a change without a worktree; the file ends with both edits, every
// time.
func TestPriorityAndApproveAtOnce(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a", "add-login"}, RequireApproval: true})
	start := func(args ...string) (*exec.Cmd, *bytes.Buffer) {
		cmd := exec.Command(tools.Tuckerbox, args...)
		cmd.Dir = f.repo
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		return cmd, &out
	}
	for i := 0; i < 8; i++ {
		_ = os.Remove(f.sidecar(f.repo, "add-login"))
		ca, oa := start("approve", "add-login")
		cp, op := start("priority", "add-login", "top")
		if err := ca.Wait(); err != nil {
			t.Fatalf("approve: %v %s", err, oa)
		}
		if err := cp.Wait(); err != nil {
			t.Fatalf("priority: %v %s", err, op)
		}
		got := f.readSidecar(f.repo, "add-login")
		if !strings.Contains(got, "[approval]") || !strings.Contains(got, "priority = 1000") {
			t.Fatalf("round %d: lost an edit:\n%s", i, got)
		}
		if st := f.approvalState(f.repo, "add-login"); st != approval.Approved {
			t.Fatalf("round %d: approval %s", i, st)
		}
	}
}

// Scenario: Run all in order. C 1000, A 2000, B none, max_parallel = 1:
// C starts first, then A, then B.
func TestRunAllInOrder(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a", "b", "c"}, MaxParallel: 1})
	testenv.WriteFile(t, f.sidecar(f.repo, "c"), "priority = 1000\n")
	testenv.WriteFile(t, f.sidecar(f.repo, "a"), "priority = 2000\n")
	f.env.SetPlan(t, map[string]testenv.Action{"c:build": {WaitFor: f.gate("c")}})
	res := f.tb("run", "--all")
	if res.code != 0 {
		t.Fatalf("run --all: %d %s %s", res.code, res.stdout, res.stderr)
	}
	f.waitState("c", "running", "build")
	f.open("c")
	for _, c := range []string{"a", "b", "c"} {
		f.waitState(c, "ready", "")
	}
	want := "c build 1,c review 1,a build 1,a review 1,b build 1,b review 1"
	if got := strings.Join(f.env.Starts(t), ","); got != want {
		t.Errorf("order:\n%s\nwant\n%s", got, want)
	}
}

// Scenario (change-runs): Unranked changes keep request order, with
// separate tuckerbox processes: A runs, then B and C (older than B) are
// started; B starts before C.
func TestUnrankedChangesKeepRequestOrderProcesses(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a", "b", "c"}, MaxParallel: 1})
	testenv.WriteFile(t, filepath.Join(f.repo, "openspec", "changes", "b", ".openspec.yaml"), "created: 2026-02-01\n")
	testenv.WriteFile(t, filepath.Join(f.repo, "openspec", "changes", "c", ".openspec.yaml"), "created: 2026-01-01\n")
	f.env.SetPlan(t, map[string]testenv.Action{"a:build": {WaitFor: f.gate("a")}})
	for _, c := range []string{"a", "b", "c"} {
		if res := f.tb("run", c); res.code != 0 {
			t.Fatalf("run %s: %s", c, res.stderr)
		}
		if c == "a" {
			f.waitState("a", "running", "build")
		}
	}
	f.waitState("c", "queued", "")
	f.open("a")
	for _, c := range []string{"a", "b", "c"} {
		f.waitState(c, "ready", "")
	}
	want := "a build 1,a review 1,b build 1,b review 1,c build 1,c review 1"
	if got := strings.Join(f.env.Starts(t), ","); got != want {
		t.Errorf("order:\n%s\nwant\n%s", got, want)
	}
}

// Scenario: Invalid priority. tuckerbox check fails, naming the file and
// priority.
func TestCheckInvalidPriority(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	testenv.WriteFile(t, f.sidecar(f.repo, "add-login"), "priority = \"high\"\n")
	res := f.tb("check")
	if res.code == 0 || !strings.Contains(res.stderr, "openspec/changes/add-login/tuckerbox.toml: priority:") {
		t.Errorf("check: %d %s", res.code, res.stderr)
	}
	testenv.WriteFile(t, f.sidecar(f.repo, "add-login"), "priority = 5\n")
	if res := f.tb("check"); res.code != 0 {
		t.Errorf("valid priority: %d %s", res.code, res.stderr)
	}
}
