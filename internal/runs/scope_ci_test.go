package runs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// ciScopeTasks ends with a group whose last task only CI can do.
const ciScopeTasks = "# Tasks\n\n" +
	"## 1. Build\n\n" +
	"- [ ] 1.1 Add the flag\n" +
	"- [ ] 1.2 Go tests pass on Linux, macOS and Windows CI\n" +
	"- [ ] 1.3 (ci) Release archives build\n" +
	"- [ ] 1.4 (ship) Archive this change\n"

// Scenario: CI task in local mode: the run is blocked naming it, as
// before.
func TestCITaskBlocksInLocalMode(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.withTasks(ciScopeTasks, "add-login")
	f.cfg.Ship = config.ShipLocal
	f.env.SetPlan(t, map[string]testenv.Action{"build": tick("1.1")})
	r := f.run(Request{Change: "add-login", Scope: "1"})
	if r.State != Blocked || r.Reason != "scope tasks not ticked: 1.2, 1.3" {
		t.Fatalf("%s %q", r.State, r.Reason)
	}
}

// Scenario: Final integration task passes on CI: the scoped run ships,
// and shipping ticks the CI tasks once CI is green, in a commit of its
// own.
func TestCITaskDoesNotHoldBackShipping(t *testing.T) {
	for _, scoped := range []string{"1", ""} {
		t.Run("scope="+scoped, func(t *testing.T) {
			f, e, api := shippingSetup(t, config.ShipMR, false, "add-login")
			f.withTasks(ciScopeTasks, "add-login")
			testenv.Git(t, f.dir, "push", "-q", "origin", "main")
			r, err := e.Run(context.Background(), Request{Change: "add-login", Scope: scoped})
			if err != nil || r.State != Ready || r.MR == nil || api.opened != 1 {
				t.Fatalf("%v %s %q", err, r.State, r.Reason)
			}
			text := readFile(t, TasksFile(f.worktree("add-login"), "add-login"))
			if !strings.Contains(text, "- [x] 1.2 Go tests") || !strings.Contains(text, "- [x] 1.3 (ci)") {
				t.Errorf("tasks.md:\n%s", text)
			}
			if msg := testenv.Git(t, f.dir, "log", "-1", "--format=%s", "add-login"); msg != "Tick add-login tasks 1.2, 1.3" {
				t.Errorf("last commit %q", msg)
			}
			if tip := testenv.Git(t, f.dir, "ls-remote", "origin", "refs/heads/add-login"); !strings.HasPrefix(tip, r.LastPush) {
				t.Errorf("remote %s, last push %s", tip, r.LastPush)
			}
		})
	}
}

// archiveDuringReview starts a scoped run whose review waits, archives the
// change on the worktree's branch meanwhile (as a builder might), then lets
// the review finish.
func archiveDuringReview(t *testing.T, f *fixture, e *Engine, scoped string) *Run {
	t.Helper()
	gate := f.gate("review")
	f.env.SetPlan(t, map[string]testenv.Action{"build": tick("1.1"), "review": {WaitFor: gate}})
	ch := make(chan *Run, 1)
	go func() {
		r, err := e.Run(context.Background(), Request{Change: "add-login", Scope: scoped})
		if err != nil {
			t.Error(err)
		}
		ch <- r
	}()
	f.waitState("add-login", Running, "review")
	wt := f.worktree("add-login")
	if err := os.MkdirAll(filepath.Join(wt, "openspec", "changes", "archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	testenv.Git(t, wt, "mv", "openspec/changes/add-login", "openspec/changes/archive/2026-11-12-add-login")
	testenv.Git(t, wt, "commit", "-q", "-m", "archive in the branch")
	open_(t, gate)
	return <-ch
}

// Scenario: Builder archived the change: the scope check reads the
// archived tasks.md and the change counts as complete.
func TestArchivedInBranchIsComplete(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.withTasks(ciScopeTasks, "add-login")
	f.cfg.Ship = config.ShipLocal
	r := archiveDuringReview(t, f, f.engine(), "1")
	if r.State != Ready || !r.Complete || r.Reason != "" {
		t.Fatalf("%s complete %v %q", r.State, r.Complete, r.Reason)
	}
}

// Scenario: Builder archived the change, shipping to a forge: it ships
// and is not archived again.
func TestArchivedInBranchShipsWithoutArchivingAgain(t *testing.T) {
	f, e, api := shippingSetup(t, config.ShipMR, true, "add-login")
	f.withTasks(ciScopeTasks, "add-login")
	testenv.Git(t, f.dir, "push", "-q", "origin", "main")
	r := archiveDuringReview(t, f, e, "1")
	if r.State != Ready || r.MR == nil || api.opened != 1 || !strings.HasSuffix(r.ArchivePath, "2026-11-12-add-login") {
		t.Fatalf("%s %q archive %q", r.State, r.Reason, r.ArchivePath)
	}
	if log := testenv.Git(t, f.dir, "log", "--format=%s", "main..add-login"); strings.Contains(log, "Archive add-login") {
		t.Errorf("archived again:\n%s", log)
	}
	archived := filepath.Join(f.worktree("add-login"), "openspec", "changes", "archive", "2026-11-12-add-login", "tasks.md")
	if text := readFile(t, archived); !strings.Contains(text, "- [x] 1.2") || !strings.Contains(text, "- [x] 1.4 (ship)") {
		t.Errorf("archived tasks.md:\n%s", text)
	}
}
