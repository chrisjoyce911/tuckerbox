package runs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

func TestShippingDependencyRequiresBaseArchiveAndIncludesItsWork(t *testing.T) {
	for _, mode := range []config.ShipMode{config.ShipMR, config.ShipMerge} {
		t.Run(string(mode), func(t *testing.T) {
			testShippingDependencyBase(t, mode)
		})
	}
}
func testShippingDependencyBase(t *testing.T, mode config.ShipMode) {
	f, e, _ := shippingSetup(t, mode, false, "b", "a")
	f.dependsOn("b", "a")
	testenv.Git(t, f.dir, "push", "origin", "main")
	dependencyState := Ready
	reason := "waiting on a (ready, not archived)"
	want := Ready
	if mode == config.ShipMerge {
		dependencyState = Shipping
		reason = "waiting on a (shipping)"
		want = Merged
	}
	f.fakeRun("a", Run{State: dependencyState, Round: 1, Complete: true})
	if mode == config.ShipMerge {
		lock, err := TryChangeLock(f.state("a").RunLock())
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Release()
	}
	awt := f.worktree("a")
	testenv.WriteFile(t, filepath.Join(awt, "a-implementation.txt"), "dependency implementation\n")
	if err := os.MkdirAll(filepath.Join(awt, "openspec", "changes", "archive"), 0755); err != nil {
		t.Fatal(err)
	}
	testenv.Git(t, awt, "mv", "openspec/changes/a", "openspec/changes/archive/2026-11-12-a")
	testenv.Git(t, awt, "add", ".")
	testenv.Git(t, awt, "commit", "-qm", "archive a away from base")
	e.DependencyRefresh = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	done := make(chan struct {
		run *Run
		err error
	}, 1)
	go func() {
		r, err := e.Run(ctx, Request{Change: "b"})
		done <- struct {
			run *Run
			err error
		}{run: r, err: err}
	}()
	f.waitReason("b", reason)
	// An archive in another worktree is insufficient, and consumes no slot.
	time.Sleep(100 * time.Millisecond)
	if r := f.current("b"); r.State != Queued {
		t.Fatal(r)
	}
	testenv.Git(t, f.dir, "merge", "--no-edit", "a")
	testenv.Git(t, f.dir, "push", "origin", "main")
	result := <-done
	if result.err != nil || result.run.State != want {
		t.Fatal(result.run, result.err)
	}
	data, err := os.ReadFile(filepath.Join(result.run.Worktree, "a-implementation.txt"))
	if err != nil || !strings.Contains(string(data), "dependency implementation") {
		t.Fatal(string(data), err)
	}
}
