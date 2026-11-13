package runs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/scope"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// lastGroupTasks ends with a group holding only a real (ship) archive task.
const lastGroupTasks = "# Tasks\n\n## 1. One\n\n- [ ] 1.1 a\n\n## 2. Two\n\n- [ ] 2.1 b\n\n## 3. Wrap-up\n\n- [ ] 3.1 (ship) Archive this change\n"

// autopilot Scenario: Last group in merge mode. Autopilot asks for each
// group with --tasks next; the last group's run finishes with every task
// ticked but the (ship) one, the run archives the change on its branch and
// the merge request merges. Then the change is archived on the base, so it
// is no longer active there and later passes (which start only active
// changes, runs.ActiveAt) start nothing for it.
func TestAutopilotLastGroupInMergeMode(t *testing.T) {
	f, e, api := shippingSetup(t, config.ShipMerge, true, "add-login")
	f.withTasks(lastGroupTasks, "add-login")
	testenv.Git(t, f.dir, "push", "-q", "origin", "main")
	ctx := context.Background()

	f.env.SetPlan(t, map[string]testenv.Action{"build": {Touch: "login.go", Tick: []string{"1.1"}}})
	r, err := e.Run(ctx, Request{Change: "add-login", Scope: scope.Next})
	if err != nil || r.State != Ready || r.Complete || r.Scope.Spec != "1" || api.merges != 0 {
		t.Fatalf("group 1: %+v %v (merges %d)", r, err, api.merges)
	}

	f.env.SetPlan(t, map[string]testenv.Action{"build": {Touch: "users.go", Tick: []string{"2.1"}}})
	r, err = e.Run(ctx, Request{Change: "add-login", Scope: scope.Next})
	if err != nil || r.State != Merged || r.Scope.Spec != "2" || api.merges != 1 {
		t.Fatalf("last group: %+v %v (merges %d)", r, err, api.merges)
	}
	archived, _ := filepath.Glob(filepath.Join(r.Worktree, "openspec", "changes", "archive", "*-add-login", "tasks.md"))
	if len(archived) != 1 {
		t.Fatalf("not archived on the branch: %v", archived)
	}
	if data, err := os.ReadFile(archived[0]); err != nil || !strings.Contains(string(data), "- [x] 3.1 (ship)") {
		t.Errorf("ship task not ticked: %v\n%s", err, data)
	}

	// The forge merges the request: the branch lands on the remote's main.
	testenv.Git(t, r.Worktree, "push", "-q", "origin", "HEAD:main")
	base, err := f.repo.Base(ctx, f.cfg.Worktree.Base, nil)
	if err != nil {
		t.Fatal(err)
	}
	rev, err := RevOf(ctx, f.repo, base)
	if err != nil {
		t.Fatal(err)
	}
	active, err := ActiveAt(ctx, f.repo, rev)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range active {
		if id == "add-login" {
			t.Errorf("add-login still active on %s: %v", base, active)
		}
	}
	if done, err := ArchivedAt(ctx, f.repo, rev); err != nil || !done["add-login"] {
		t.Errorf("add-login not archived on %s: %v %v", base, done, err)
	}
}
