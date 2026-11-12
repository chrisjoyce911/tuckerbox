package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// withRemote gives the fixture's repository a bare origin and returns a
// second clone of it: another machine's checkout.
func (f *cliFixture) withRemote() string {
	f.t.Helper()
	parent := filepath.Dir(f.repo)
	bare := filepath.Join(parent, "origin.git")
	testenv.Git(f.t, parent, "init", "-q", "--bare", "-b", "main", bare)
	testenv.Git(f.t, f.repo, "remote", "add", "origin", bare)
	testenv.Git(f.t, f.repo, "push", "-q", "-u", "origin", "main")
	other := filepath.Join(f.t.TempDir(), "repo")
	testenv.Git(f.t, parent, "clone", "-q", bare, other)
	for _, kv := range [][2]string{{"user.name", "Other"}, {"user.email", "o@example.invalid"}, {"commit.gpgsign", "false"}} {
		testenv.Git(f.t, other, "config", kv[0], kv[1])
	}
	return other
}

// approval-publish: Approved on one machine, run on another.
func TestApprovedOnOneMachineRunOnAnother(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	other := f.withRemote()
	if res := tbIn(t, other, "run", "add-login"); res.code == 0 {
		t.Fatalf("ran before any approval: %s", res.stdout)
	}
	f.mustApprove("add-login")
	res := tbIn(t, other, "run", "--foreground", "add-login")
	if res.code != 0 {
		t.Fatalf("run on the other machine: %d %s", res.code, res.stderr)
	}
	if out := testenv.Git(t, f.repo, "log", "--oneline", "main"); strings.Count(out, "\n") != 0 {
		t.Errorf("approval committed to main:\n%s", out)
	}
}

// approval-publish: Old approval in the sidecar; Unapproved after an old
// approval.
func TestOldSidecarApprovalAndWithdrawal(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-users"}, RequireApproval: true})
	if _, err := approval.Approve(approval.DirOf(f.repo, "add-users"), "Old", time.Now()); err != nil {
		t.Fatal(err)
	}
	f.commit("old approval in the sidecar")
	if st := f.approvalState(f.repo, "add-users"); st != approval.Approved {
		t.Fatalf("sidecar approval: %s", st)
	}
	if res := f.tb("unapprove", "add-users"); res.code != 0 || !strings.Contains(res.stdout, "withdrew") {
		t.Fatalf("unapprove: %d %s %s", res.code, res.stdout, res.stderr)
	}
	if st := f.approvalState(f.repo, "add-users"); st != approval.None {
		t.Errorf("after withdrawal: %s", st)
	}
	res := f.tb("run", "add-users")
	if res.code == 0 || !strings.Contains(res.stderr, "not approved") {
		t.Errorf("run after withdrawal: %d %s", res.code, res.stderr)
	}
	if out := testenv.Git(t, f.repo, "status", "--porcelain"); out != "" {
		t.Errorf("unapprove changed files: %s", out)
	}
}

// approval-publish task 2.6: check notes a change approved both on the ref
// and in its sidecar with different hashes.
func TestCheckNotesTwoApprovals(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	testenv.WriteFile(t, filepath.Join(f.changeDir("add-login"), "tuckerbox.toml"),
		"[approval]\nhash = \"sha256:old\"\nby = \"Old\"\nat = 2026-11-01T09:00:00+10:00\n")
	f.commit("old approval")
	f.mustApprove("add-login")
	res := f.tb("check")
	if res.code != 0 || !strings.Contains(res.stdout, "note: add-login is approved on refs/tuckerbox/approvals and has a different [approval]") {
		t.Errorf("check: %d %s %s", res.code, res.stdout, res.stderr)
	}
}
