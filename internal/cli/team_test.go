package cli

import (
	"bytes"
	"context"
	"fmt"
	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type teamAPI struct {
	forge.TeamClient
	protected     bool
	protectionErr error
	opens         int
}

func (f *teamAPI) Login(context.Context) (string, error) { return "sam", nil }
func (f *teamAPI) Access(context.Context) (forge.Access, error) {
	return forge.Access{DefaultBranch: "main", Push: true, Read: true}, nil
}
func (f *teamAPI) ProtectedBase(context.Context, string) (bool, error) {
	return f.protected, f.protectionErr
}
func (f *teamAPI) FindMR(context.Context, string) (*forge.MR, error) { return nil, nil }
func (f *teamAPI) OpenMR(context.Context, forge.OpenRequest) (*forge.MR, error) {
	f.opens++
	return nil, fmt.Errorf("unexpected open")
}
func teamCLI(t *testing.T) (*cliFixture, App, *teamAPI, *bytes.Buffer) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true, Extra: "\n[team]\napprovers = [\"sam\"]\n[forge]\nkind = \"github\"\n"})
	p := filepath.Join(f.repo, config.RepoFile)
	b, _ := os.ReadFile(p)
	s := strings.Replace(string(b), "ship = \"local\"", "ship = \"mr\"", 1)
	testenv.WriteFile(t, p, "mode = \"team\"\n"+s)
	f.commit("team config")
	t.Setenv(config.TestForgeURLEnv, "https://github.com/sam/app.git")
	var out, stderr bytes.Buffer
	api := &teamAPI{protected: true}
	a := App{Stdout: &out, Stderr: &stderr, Getwd: func() (string, error) { return f.repo, nil }, Forge: func(context.Context, *config.Config) (forge.Client, func(string) string, error) {
		return api, func(s string) string { return s }, nil
	}}
	return f, a, api, &stderr
}
func TestTeamApproveUnapproveIgnoreApprovalRef(t *testing.T) {
	f, a, _, stderr := teamCLI(t)
	if code := a.approve([]string{"add-login"}); code != 0 {
		t.Fatal(code, stderr.String())
	}
	d := approval.DirOf(f.repo, "add-login")
	data, err := d.Read(approval.SidecarName)
	if err != nil {
		t.Fatal(err)
	}
	ap, err := approval.ParseSidecar(data)
	if err != nil || ap.Login != "sam" {
		t.Fatal(ap, err)
	}
	entries, err := (approval.Store{Dir: f.repo}).Entries(context.Background())
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
	// A withdrawal on the approvals ref must not shadow the team sidecar.
	err = (approval.Store{Dir: f.repo}).Record(context.Background(), "add-login", approval.Entry{State: approval.EntryWithdrawn, By: "Elsewhere", At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	st, err := currentApproval(context.Background(), f.repo, "add-login")
	if err != nil || st.State != approval.Approved {
		t.Fatal(st, err)
	}
	if code := a.unapprove([]string{"add-login"}); code != 0 {
		t.Fatal(code, stderr.String())
	}
	if _, err = d.Read(approval.SidecarName); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	entries, _ = (approval.Store{Dir: f.repo}).Entries(context.Background())
	if entries["add-login"].By != "Elsewhere" {
		t.Fatal(entries)
	}
}
func TestTeamRunGateRejectsForgedRetainedBranchWithoutMutation(t *testing.T) {
	f, a, api, _ := teamCLI(t)
	f.withRemote()
	repo, cfg, ok := a.loadRepo("run", f.repo)
	if !ok {
		t.Fatal("config")
	}
	testenv.Git(t, f.repo, "checkout", "-qb", "add-login")
	if code := a.approve([]string{"add-login"}); code != 0 {
		t.Fatal(code)
	}
	f.commit("forged approval")
	before := testenv.Git(t, f.repo, "worktree", "list", "--porcelain")
	g := runGate{a: a, repo: repo, cfg: cfg}
	_, err := g.check(context.Background(), "add-login")
	if err == nil || !strings.Contains(err.Error(), "base branch has no approval") {
		t.Fatal(err)
	}
	after := testenv.Git(t, f.repo, "worktree", "list", "--porcelain")
	if before != after || api.opens != 0 {
		t.Fatal(before, after, api.opens)
	}
}
func TestTeamCheckAndRunRefuseUnprotectedOrUnreadableBase(t *testing.T) {
	for _, unreadable := range []bool{false, true} {
		t.Run(fmt.Sprint(unreadable), func(t *testing.T) {
			f, a, api, stderr := teamCLI(t)
			f.withRemote()
			api.protected = false
			if unreadable {
				api.protectionErr = fmt.Errorf("protection couldn't be confirmed: token needs Administration: read")
			}
			if a.check() == 0 || !strings.Contains(stderr.String(), "team approvals need a protected base branch") {
				t.Fatal(stderr.String())
			}
			repo, cfg, ok := a.loadRepo("run", f.repo)
			if !ok {
				t.Fatal("config")
			}
			err := a.teamGate(context.Background(), repo, cfg, "add-login", runs.StartPoint{Worktree: f.repo}, false)
			if err == nil || !strings.Contains(err.Error(), "protected base branch") {
				t.Fatal(err)
			}
			f.noWorktree("add-login")
		})
	}
}
