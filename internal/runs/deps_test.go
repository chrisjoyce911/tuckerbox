package runs

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// dependsOn commits change's tuckerbox.toml on main.
func (f *fixture) dependsOn(change string, ids ...string) {
	f.t.Helper()
	toml := `depends_on = ["` + strings.Join(ids, `", "`) + `"]` + "\n\n[approval]\nhash = \"sha256:00\"\n"
	testenv.WriteFile(f.t, filepath.Join(f.dir, "openspec", "changes", change, "tuckerbox.toml"), toml)
	testenv.Git(f.t, f.dir, "add", "-A")
	testenv.Git(f.t, f.dir, "commit", "-q", "-m", change+" dependencies")
}

func (f *fixture) waitReason(change, reason string) {
	f.t.Helper()
	waitFor(f.t, change+" queued: "+reason, func() bool {
		r := f.current(change)
		return r != nil && r.State == Queued && r.Reason == reason
	})
}

// Scenario: Dependency still building. B depends on A, which is in its
// build step: B is queued with reason "waiting on a (build)" and starts
// once A is ready.
func TestDependencyStillBuilding(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"a", "b"}, MaxParallel: 2})
	f.dependsOn("b", "a")
	gate := f.gate("a")
	f.env.SetPlan(t, map[string]testenv.Action{"a:build": {WaitFor: gate}})
	da := f.start(context.Background(), Request{Change: "a"})
	f.waitState("a", Running, "build")
	db := f.start(context.Background(), Request{Change: "b"})
	f.waitReason("b", "waiting on a (build)")
	time.Sleep(200 * time.Millisecond)
	if r := f.current("b"); r.State != Queued {
		t.Fatalf("b is %s while a builds", r.State)
	}
	open_(t, gate)
	for _, ch := range []<-chan result{da, db} {
		if res := <-ch; res.err != nil || res.run.State != Ready || res.run.Reason != "" {
			t.Fatalf("%+v %v", res.run, res.err)
		}
	}
	wantStarts(t, f.env, "a build 1", "a review 1", "b build 1", "b review 1")
}

// A run waiting on dependencies holds no slot and does not hold up the
// queue: with max_parallel = 1, C runs while B waits on A, which has not
// started. B starts once A has run to ready.
func TestDependencyWaitHoldsNoSlot(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"a", "b", "c"}})
	f.dependsOn("b", "a")
	db := f.start(context.Background(), Request{Change: "b"})
	f.waitReason("b", "waiting on a (not started)")
	if r := f.run(Request{Change: "c"}); r.State != Ready {
		t.Fatalf("c: %+v", r)
	}
	if r := f.current("b"); r.State != Queued {
		t.Fatalf("b is %s before a ran", r.State)
	}
	if r := f.run(Request{Change: "a"}); r.State != Ready {
		t.Fatalf("a: %+v", r)
	}
	if res := <-db; res.err != nil || res.run.State != Ready {
		t.Fatalf("b: %+v %v", res.run, res.err)
	}
	wantStarts(t, f.env, "c build 1", "c review 1", "a build 1", "a review 1", "b build 1", "b review 1")
}

// Scenario: Dependency archived. B depends on A, which has been archived:
// B starts without waiting.
func TestDependencyArchived(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"b"}})
	testenv.WriteFile(t, filepath.Join(f.dir, "openspec", "changes", "archive", "2026-11-01-a", "proposal.md"), "# a\n")
	f.dependsOn("b", "a")
	if r := f.run(Request{Change: "b"}); r.State != Ready {
		t.Fatalf("b: %+v", r)
	}
	wantStarts(t, f.env, "b build 1", "b review 1")
}

// A run waiting on dependencies can be stopped, and one that depends on a
// change that does not exist fails rather than waiting for ever.
func TestDependencyStopAndUnknown(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"a", "b"}})
	f.dependsOn("b", "a")
	ctx, cancel := context.WithCancel(context.Background())
	db := f.start(ctx, Request{Change: "b"})
	f.waitReason("b", "waiting on a (not started)")
	cancel()
	if res := <-db; res.err != nil || res.run.State != Stopped || !strings.Contains(res.run.Reason, "waiting on dependencies") {
		t.Fatalf("b: %+v %v", res.run, res.err)
	}

	f.dependsOn("a", "add-login")
	r := f.run(Request{Change: "a"})
	if r.State != Failed || !strings.Contains(r.Reason, "add-login") {
		t.Fatalf("a: %+v", r)
	}
	wantStarts(t, f.env)
}

// A dependency that leaves ready while the run waits for a slot is seen
// once the slot is taken: the run gives the slot back and waits again, so
// the dependency's rerun goes first. With max_parallel = 1: A is ready, C
// holds the slot, B (depends on A) queues for it, then A is rerun and
// queues behind B. When C ends, B must not start before A is ready again.
func TestDependencyRecheckedAfterSlot(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"a", "b", "c"}})
	f.dependsOn("b", "a")
	if r := f.run(Request{Change: "a"}); r.State != Ready {
		t.Fatalf("a: %+v", r)
	}
	gate := f.gate("c")
	f.env.SetPlan(t, map[string]testenv.Action{"c:build": {WaitFor: gate}})
	dc := f.start(context.Background(), Request{Change: "c"})
	f.waitState("c", Running, "build")
	db := f.start(context.Background(), Request{Change: "b"}) // in the queue
	da := f.start(context.Background(), Request{Change: "a", Mode: Rerun, Step: "build"})
	f.waitState("a", Queued, "build")
	open_(t, gate)
	for name, ch := range map[string]<-chan result{"a": da, "b": db, "c": dc} {
		if res := <-ch; res.err != nil || res.run.State != Ready {
			t.Fatalf("%s: %+v %v", name, res.run, res.err)
		}
	}
	wantStarts(t, f.env, "a build 1", "a review 1", "c build 1", "c review 1",
		"a build 2", "a review 2", "b build 1", "b review 1")
}
