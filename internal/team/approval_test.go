package team

import (
	"context"
	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	root := t.TempDir()
	testenv.Git(t, root, "init", "-q", "-b", "main")
	testenv.Git(t, root, "config", "user.name", "Test")
	testenv.Git(t, root, "config", "user.email", "test@example.invalid")
	testenv.WriteFile(t, filepath.Join(root, config.RepoFile), "mode = \"team\"\n[team]\napprovers = [\"sam\"]\n")
	testenv.WriteFile(t, filepath.Join(string(approval.DirOf(root, "login")), "proposal.md"), "approved text\n")
	testenv.Git(t, root, "add", ".")
	testenv.Git(t, root, "commit", "-qm", "base")
	return root, testenv.Git(t, root, "rev-parse", "HEAD")
}
func approve(t *testing.T, root, login string) {
	t.Helper()
	d := approval.DirOf(root, "login")
	a, err := approval.Approve(d, "Test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	a.Login = login
	old, _ := d.Read(approval.SidecarName)
	b, err := approval.SetApproval(old, a)
	if err != nil {
		t.Fatal(err)
	}
	if err = approval.WriteSidecar(d.SidecarPath(), b); err != nil {
		t.Fatal(err)
	}
}
func commit(t *testing.T, root string) string {
	testenv.Git(t, root, "add", ".")
	testenv.Git(t, root, "commit", "-qm", "approval")
	return testenv.Git(t, root, "rev-parse", "HEAD")
}
func TestProtectedBaseApprovalIgnoresForgedBranchAndWorktree(t *testing.T) {
	for _, scenario := range []string{"forged retained branch", "edited worktree", "unlisted approver", "ref entry"} {
		t.Run(scenario, func(t *testing.T) {
			root, base := fixture(t)
			ctx := context.Background()
			d := approval.DirOf(root, "login")
			switch scenario {
			case "forged retained branch":
				testenv.Git(t, root, "checkout", "-qb", "login")
				approve(t, root, "sam")
				commit(t, root)
			case "edited worktree":
				approve(t, root, "sam")
				base = commit(t, root)
				testenv.WriteFile(t, filepath.Join(string(d), "proposal.md"), "forged text")
				approve(t, root, "sam")
			case "unlisted approver":
				approve(t, root, "alex")
				base = commit(t, root)
			case "ref entry":
				hash, _ := approval.Hash(d)
				err := (approval.Store{Dir: root}).Record(ctx, "login", approval.Entry{State: approval.EntryApproved, Hash: hash, By: "sam", At: time.Now()})
				if err != nil && !strings.Contains(err.Error(), "remote") {
					t.Fatal(err)
				}
			}
			_, err := Judge(ctx, root, base, "login", d, false)
			if err == nil {
				t.Fatal("forged approval accepted")
			}
			want := "base branch has no approval"
			if scenario == "edited worktree" {
				want = "files don't match"
			}
			if scenario == "unlisted approver" {
				want = "alex is not an approver"
			}
			if !strings.Contains(err.Error(), want) {
				t.Fatal(err)
			}
		})
	}
}
func TestResumeAndRerunCheckBaseSettingsAllowPlanText(t *testing.T) {
	for _, mode := range []string{"resume", "rerun", "plan text"} {
		t.Run(mode, func(t *testing.T) {
			root, _ := fixture(t)
			d := approval.DirOf(root, "login")
			testenv.WriteFile(t, d.SidecarPath(), "[steps.build]\nagent = \"claude\"\n")
			approve(t, root, "sam")
			base := commit(t, root)
			if mode == "plan text" {
				testenv.WriteFile(t, filepath.Join(string(d), "design.md"), "plan rewrote this")
			} else {
				b, _ := d.Read(approval.SidecarName)
				testenv.WriteFile(t, d.SidecarPath(), strings.Replace(string(b), "agent = \"claude\"", "agent = \"codex\"", 1))
				approve(t, root, "sam")
			}
			_, err := Judge(context.Background(), root, base, "login", d, true)
			if mode == "plan text" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "step.build.agent") {
				t.Fatal(err)
			}
		})
	}
}

type protectedForge struct {
	forge.TeamClient
	protected bool
	err       error
}

func (f protectedForge) Access(context.Context) (forge.Access, error) {
	return forge.Access{DefaultBranch: "main"}, nil
}
func (f protectedForge) ProtectedBase(context.Context, string) (bool, error) {
	return f.protected, f.err
}
func TestProtectionRequiredBeforeFetch(t *testing.T) {
	root, _ := fixture(t)
	for _, f := range []protectedForge{{}, {err: os.ErrPermission}} {
		_, _, err := Base(context.Background(), root, &config.Config{}, f)
		if err == nil || !strings.Contains(err.Error(), "team approvals need a protected base branch") {
			t.Fatal(err)
		}
	}
}
