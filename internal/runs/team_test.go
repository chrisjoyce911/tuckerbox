package runs

import (
	"context"
	"fmt"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
	"strings"
	"testing"
)

type teamForge struct {
	*shippingForge
	login            string
	updates, readies int
}

func (f *teamForge) Login(context.Context) (string, error)               { return f.login, nil }
func (f *teamForge) ProtectedBase(context.Context, string) (bool, error) { return true, nil }
func (f *teamForge) ListOpenMRs(context.Context) ([]forge.MR, error)     { return nil, nil }
func (f *teamForge) OpenMR(ctx context.Context, r forge.OpenRequest) (*forge.MR, error) {
	m, err := f.shippingForge.OpenMR(ctx, r)
	if err == nil {
		f.mu.Lock()
		f.mr.Author = f.login
		f.mr.Draft = r.Draft
		m.Author = f.login
		m.Draft = r.Draft
		f.mu.Unlock()
	}
	return m, err
}
func (f *teamForge) UpdateMR(_ context.Context, _ int, r forge.OpenRequest) (*forge.MR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates++
	f.mr.Title = r.Title
	f.mr.Body = r.Body
	m := *f.mr
	return &m, nil
}
func (f *teamForge) MarkReady(context.Context, int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readies++
	f.mr.Draft = false
	return nil
}
func teamSetup(t *testing.T) (*fixture, *Engine, *teamForge) {
	f, e, shipping := shippingSetup(t, config.ShipMR, false, "add-login")
	f.cfg.Mode = config.ModeTeam
	api := &teamForge{shippingForge: shipping, login: "sam"}
	e.Forge = func(context.Context) (forge.Client, func(string) string, error) {
		return api, func(s string) string { return s }, nil
	}
	return f, e, api
}
func TestTeamNextScopeAndRestReuseDraft(t *testing.T) {
	for _, rest := range []bool{false, true} {
		t.Run(fmt.Sprint(rest), func(t *testing.T) {
			f, e, api := teamSetup(t)
			f.withTasks(scopedTasks, "add-login")
			testenv.Git(t, f.dir, "push", "origin", "main")
			f.env.SetPlan(t, map[string]testenv.Action{"build": tick("1.1", "1.2")})
			r, err := e.Run(context.Background(), Request{Change: "add-login", Scope: "1"})
			if err != nil || r.State != Ready || api.opened != 1 || !api.mr.Draft {
				t.Fatal(r, err, api)
			}
			id, ok := ship.ParseMarker(api.mr.Body)
			if !ok || id.By != "sam" || api.mr.Author != "sam" {
				t.Fatal(id, api.mr)
			}
			if msg := testenv.Git(t, r.Worktree, "show", "-s", "--format=%B", r.LastPush); !strings.Contains(msg, "Start add-login") || !strings.Contains(msg, "[skip ci]") {
				t.Fatal(msg)
			}
			before := r.LastPush
			// Block the workflow in a later scope to inspect the unchanged draft and tip.
			f.env.SetPlan(t, map[string]testenv.Action{"build": {Status: "blocked", Summary: "wait"}})
			scope := "2"
			if rest {
				scope = ""
			}
			r, err = e.Run(context.Background(), Request{Change: "add-login", Scope: scope})
			if err != nil || r.State != Blocked || r.LastPush != before || api.opened != 1 || api.readies != 0 {
				t.Fatal(r, err, api)
			}
			if tip := testenv.Git(t, f.dir, "ls-remote", "origin", "refs/heads/add-login"); !strings.HasPrefix(tip, before) {
				t.Fatal(tip)
			}
		})
	}
}
func TestTeamContinuationRefusesClosedForeignCopiedAndMovedClaims(t *testing.T) {
	for _, scenario := range []string{"closed", "foreign", "copied", "moved", "ready"} {
		t.Run(scenario, func(t *testing.T) {
			f, e, api := teamSetup(t)
			f.withTasks(scopedTasks, "add-login")
			testenv.Git(t, f.dir, "push", "origin", "main")
			f.env.SetPlan(t, map[string]testenv.Action{"build": tick("1.1", "1.2")})
			r, err := e.Run(context.Background(), Request{Change: "add-login", Scope: "1"})
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "ready":
				api.mr.Draft = false
			case "closed":
				api.mr.State = "closed"
			case "foreign":
				api.login = "alex"
			case "copied":
				api.mr.Author = "alex"
			case "moved":
				testenv.Git(t, r.Worktree, "commit", "--allow-empty", "-m", "someone else")
				testenv.Git(t, r.Worktree, "push", "origin", "add-login")
			}
			_, err = e.Run(context.Background(), Request{Change: "add-login", Scope: "2"})
			if err == nil {
				t.Fatal("accepted", scenario)
			}
			if scenario == "closed" && !strings.Contains(err.Error(), "merge request closed") {
				t.Fatal(err)
			}
		})
	}
}
func TestTeamLastStepMarksExistingDraftReady(t *testing.T) {
	f, e, api := teamSetup(t)
	r, err := e.Run(context.Background(), Request{Change: "add-login"})
	if err != nil || r.State != Ready || api.opened != 1 || api.readies != 1 || api.updates != 1 || api.mr.Draft {
		t.Fatal(r, err, api)
	}
	if r.TeamLogin != "sam" {
		t.Fatal(r)
	}
	_ = f
}

func TestTeamModeChangeKeepsDraftClaim(t *testing.T) {
	f, e, api := teamSetup(t)
	f.withTasks(scopedTasks, "add-login")
	testenv.Git(t, f.dir, "push", "origin", "main")
	f.env.SetPlan(t, map[string]testenv.Action{"build": tick("1.1", "1.2")})
	r, err := e.Run(context.Background(), Request{Change: "add-login", Scope: "1"})
	if err != nil || r.StartedMode() != config.ModeTeam {
		t.Fatal(r, err)
	}
	f.cfg.Mode = config.ModeSolo
	api.mr.State = "closed"
	_, err = e.Run(context.Background(), Request{Change: "add-login", Scope: "2"})
	if err == nil || !strings.Contains(err.Error(), "merge request closed") {
		t.Fatal("mode change bypassed team claim", err)
	}
}

func TestTeamStartCommitIsEmptyWithStagedWork(t *testing.T) {
	f, e, _ := teamSetup(t)
	ctx := context.Background()
	ws, err := f.repo.EnsureWorktree(ctx, f.cfg.Worktree, "add-login", nil)
	if err != nil {
		t.Fatal(err)
	}
	testenv.WriteFile(t, ws.Path+"/staged.txt", "user work")
	testenv.Git(t, ws.Path, "add", "staged.txt")
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Status: "blocked", Summary: "wait"}})
	r, err := e.Run(ctx, Request{Change: "add-login"})
	if err != nil || r.State != Blocked {
		t.Fatal(r, err)
	}
	if files := testenv.Git(t, ws.Path, "diff-tree", "--no-commit-id", "--name-only", "-r", r.LastPush); files != "" {
		t.Fatal("start commit contains staged work", files)
	}
	if status := testenv.Git(t, ws.Path, "status", "--porcelain"); !strings.Contains(status, "staged.txt") {
		t.Fatal("staged work lost", status)
	}
}
