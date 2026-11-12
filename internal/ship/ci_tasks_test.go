package ship

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
)

const ciTasks = "# Tasks\n\n## 1. Build\n\n" +
	"- [x] 1.1 Add the flag\n" +
	"- [ ] 1.2 Go tests pass on Linux, macOS and Windows CI\n" +
	"- [ ] 1.3 (ci) Release archives build\n" +
	"- [ ] 1.4 (ship) Archive this change\n"

// gitCommit is a Commit hook that commits everything in dir and records
// each message.
func gitCommit(t *testing.T, dir string, msgs *[]string) func(context.Context, string) (bool, error) {
	return func(_ context.Context, msg string) (bool, error) {
		gitTest(t, dir, "add", "-A")
		if gitTest(t, dir, "status", "--porcelain") == "" {
			return false, nil
		}
		gitTest(t, dir, "commit", "-q", "-m", msg)
		*msgs = append(*msgs, msg)
		return true, nil
	}
}

func writeTasks(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Scenario: CI task ticked once CI is green.
func TestCITasksTickedOnceCIIsGreen(t *testing.T) {
	for _, mode := range []config.ShipMode{config.ShipMR, config.ShipMerge} {
		t.Run(string(mode), func(t *testing.T) {
			e, api, f := stage(t, mode)
			path := filepath.Join(f.one, "openspec", "changes", "add-login", "tasks.md")
			writeTasks(t, path, ciTasks)
			var msgs []string
			e.Commit = gitCommit(t, f.one, &msgs)
			out := e.Run(context.Background(), false)
			want := map[config.ShipMode]string{config.ShipMR: "ready", config.ShipMerge: "merged"}[mode]
			if out.State != want {
				t.Fatalf("outcome %+v", out)
			}
			text := readText(t, path)
			if !strings.Contains(text, "- [x] 1.2 Go tests") || !strings.Contains(text, "- [x] 1.3 (ci)") || !strings.Contains(text, "- [ ] 1.4 (ship)") {
				t.Errorf("tasks.md:\n%s", text)
			}
			if len(msgs) != 2 || msgs[1] != "Tick add-login tasks 1.2, 1.3" {
				t.Errorf("commits %q", msgs)
			}
			head := gitTest(t, f.one, "rev-parse", "HEAD")
			if e.Record.LastPush != head {
				t.Errorf("tick commit not pushed: head %s last push %s", head, e.Record.LastPush)
			}
			if mode == config.ShipMR {
				if remote := gitTest(t, f.one, "rev-parse", "origin/add-login"); remote != head {
					t.Errorf("remote branch at %s, want %s", remote, head)
				}
			}
			if mode == config.ShipMerge && (api.mergeCalls != 1 || api.mergedSHA != head) {
				t.Errorf("merged %d at %s, want the tick commit %s", api.mergeCalls, api.mergedSHA, head)
			}
		})
	}
}

// Scenario: CI fails: CI tasks stay unticked.
func TestCITasksNotTickedWhenCIFails(t *testing.T) {
	e, api, f := stage(t, config.ShipMR)
	api.ci = "red"
	path := filepath.Join(f.one, "openspec", "changes", "add-login", "tasks.md")
	writeTasks(t, path, ciTasks)
	var msgs []string
	e.Commit = gitCommit(t, f.one, &msgs)
	if out := e.Run(context.Background(), false); out.State != "blocked" {
		t.Fatalf("outcome %+v", out)
	}
	if text := readText(t, path); text != ciTasks {
		t.Errorf("tasks.md changed:\n%s", text)
	}
}

// Scenario: Change archived on its branch is not archived again; its
// archived tasks.md gets the CI ticks.
func TestArchivedOnBranchIsNotArchivedAgain(t *testing.T) {
	e, _, f := stage(t, config.ShipMR)
	e.Config.Shipping.Archive = true
	path := filepath.Join(f.one, "openspec", "changes", "archive", "2026-11-12-add-login", "tasks.md")
	writeTasks(t, path, ciTasks)
	archived := false
	e.Archive = func(context.Context, string) (string, error) { archived = true; return "", nil }
	var msgs []string
	e.Commit = gitCommit(t, f.one, &msgs)
	out := e.Run(context.Background(), false)
	if out.State != "ready" || archived || !strings.HasSuffix(e.Record.ArchivePath, "2026-11-12-add-login") {
		t.Fatalf("outcome %+v archived again %v path %q", out, archived, e.Record.ArchivePath)
	}
	if text := readText(t, path); !strings.Contains(text, "- [x] 1.2") || !strings.Contains(text, "- [x] 1.4 (ship)") {
		t.Errorf("archived tasks.md:\n%s", text)
	}
}
