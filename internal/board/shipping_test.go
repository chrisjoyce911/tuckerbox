package board

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

func TestShippingColumnsRespectLayout(t *testing.T) {
	for _, mode := range []config.ShipMode{config.ShipMR, config.ShipMerge} {
		cfg := &config.Config{Ship: mode, BoardColumns: config.BoardSteps, RequireApproval: true, Steps: []config.Step{{ID: "build"}, {ID: "review"}}}
		var names []string
		for _, col := range columns(cfg) {
			names = append(names, col.Name)
		}
		if got := strings.Join(names, ","); got != "Proposed,Approved,Queued,Build,Review,Shipping,Ready,Done" {
			t.Fatal(got)
		}
		cfg.BoardColumns = config.BoardCompact
		names = nil
		for _, col := range columns(cfg) {
			names = append(names, col.Name)
		}
		if got := strings.Join(names, ","); got != "Proposed,Approved,In progress,Shipping,Ready,Done" {
			t.Fatal(got)
		}
	}
}
func TestShippingCardsFromStateWithoutForgeCalls(t *testing.T) {
	for _, state := range []runs.State{runs.Shipping, runs.Blocked, runs.Failed, runs.Ready, runs.Merged} {
		t.Run(string(state), func(t *testing.T) {
			testTools.Setup(t)
			root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"add-login"}, Extra: "[board]\ncolumns='steps'\n[shipping]\nfix_ci='fix_ci'\n[steps.fix_ci]\nagent='fake'\nrole='fixer'\n"})
			path := filepath.Join(root, config.RepoFile)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			testenv.WriteFile(t, path, strings.Replace(string(data), `ship = "local"`, `ship = "mr"`, 1))
			// Approval-ref sync may fetch, so the real remote is local. Config
			// discovery alone receives a synthetic forge identity.
			remote := filepath.Join(t.TempDir(), "remote.git")
			testenv.Git(t, root, "clone", "--bare", root, remote)
			testenv.Git(t, root, "remote", "add", "origin", remote)
			worktree := filepath.Join(filepath.Dir(root), "repo-worktrees", "add-login")
			testenv.Git(t, root, "worktree", "add", "-qb", "add-login", worktree)
			st, err := statedir.Resolve(context.Background(), worktree)
			if err != nil {
				t.Fatal(err)
			}
			mrState := "open"
			if state == runs.Merged {
				mrState = "merged"
				source := filepath.Join(worktree, "openspec", "changes", "add-login")
				destination := filepath.Join(worktree, "openspec", "changes", "archive", "2026-11-12-add-login")
				if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(source, destination); err != nil {
					t.Fatal(err)
				}
			}
			r := &runs.Run{Record: ship.Record{ShippingStarted: true, MR: &ship.MRInfo{Link: "https://github.com/example/shop/pull/12", Number: 12, State: mrState}, CI: &ship.CIInfo{State: "pending", Commit: "abc123"}, ShipRound: 2}, Change: "add-login", Worktree: worktree, Branch: "add-login", State: state, Step: "fix_ci", Round: 2, Since: time.Now()}
			if state == runs.Ready {
				r.Step = ""
				r.CI.State = "green"
			}
			if err := runs.WriteRun(st.RunFile(), r); err != nil {
				t.Fatal(err)
			}
			if state == runs.Shipping {
				lock, err := runs.TryChangeLock(st.RunLock())
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Release()
			}
			env := config.SystemEnv()
			realGit := env.Git
			env.Git = func(dir string, args ...string) (string, error) {
				if strings.Join(args, " ") == "config --get remote.origin.url" || strings.Join(args, " ") == "remote get-url --push -- origin" {
					return "https://github.com/example/shop.git", nil
				}
				return realGit(dir, args...)
			}
			doc, err := Read(context.Background(), root, "add-login", env)
			if err != nil {
				t.Fatal(err)
			}
			c := card(t, doc, "add-login")
			want := "shipping"
			if state == runs.Ready {
				want = "ready"
			}
			if state == runs.Merged {
				want = "done"
			}
			if c.Column != want || c.MR == nil || c.MR.Number != 12 || c.CI.Commit != "abc123" || c.ShipRound != 2 || c.MaxRounds != 3 || c.Error != "" {
				t.Fatal(c)
			}
			if state == runs.Shipping && (c.Step != "fix_ci" || c.Agent != "fake") {
				t.Fatal(c)
			}
			if state == runs.Ready && c.Badge != "waiting for merge" {
				t.Fatal(c)
			}
			if state == runs.Merged && (c.State != "merged" || doc.Detail == nil || doc.Detail.Proposal == "") {
				t.Fatal(c, doc.Detail)
			}
		})
	}
}
