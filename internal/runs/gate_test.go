package runs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// The engine's Gate judges a new worktree's commit before making it, and
// the worktree again with the change lock held, before the run state
// changes. A refusal there leaves no run.
func TestGateRunsUnderTheChangeLock(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	ctx := context.Background()
	head := testenv.Git(t, f.dir, "rev-parse", "HEAD")
	var calls []StartPoint
	refuse := errors.New("not approved")
	e := f.engine()
	e.Gate = func(ctx context.Context, change string, at StartPoint) error {
		calls = append(calls, at)
		if at.Worktree == "" {
			if _, err := os.Stat(f.worktree(change)); err == nil {
				t.Error("worktree made before the commit was judged")
			}
			return nil
		}
		st, err := statedir.Resolve(ctx, at.Worktree)
		if err != nil {
			t.Fatal(err)
		}
		if !Active(st.RunLock()) {
			t.Error("gate judged the worktree without the change lock")
		}
		if _, err := ReadRun(st.RunFile()); !errors.Is(err, ErrNoRun) {
			t.Errorf("run state written before the gate: %v", err)
		}
		return refuse
	}
	if _, err := e.Run(ctx, Request{Change: "fix-login", Mode: Start}); !errors.Is(err, refuse) {
		t.Fatalf("run: %v", err)
	}
	if len(calls) != 2 || calls[0].Rev != head || calls[0].Ref != "main" || calls[1].Worktree == "" {
		t.Errorf("gate calls %+v", calls)
	}
	st, _ := statedir.Resolve(ctx, f.worktree("fix-login"))
	if _, err := ReadRun(st.RunFile()); !errors.Is(err, ErrNoRun) {
		t.Errorf("refused run left state: %v", err)
	}
}

// A new worktree is made at the commit the caller checked, not at where the
// base is now; a retained change branch is judged and used as is.
func TestStartRevPinsTheWorktree(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login", "old"}})
	ctx := context.Background()
	checked := testenv.Git(t, f.dir, "rev-parse", "HEAD")
	testenv.WriteFile(t, f.dir+"/README.md", "moved on\n")
	testenv.Git(t, f.dir, "commit", "-q", "-am", "base moves on")
	testenv.Git(t, f.dir, "branch", "old", checked)

	var judged []StartPoint
	e := f.engine()
	e.Gate = func(_ context.Context, _ string, at StartPoint) error { judged = append(judged, at); return nil }
	if _, err := e.Run(ctx, Request{Change: "fix-login", Mode: Start, StartRev: checked}); err != nil {
		t.Fatal(err)
	}
	wt := f.worktree("fix-login")
	testenv.Git(t, wt, "merge-base", "--is-ancestor", checked, "HEAD")
	if out, err := exec.Command("git", "-C", wt, "merge-base", "--is-ancestor", "main", "HEAD").CombinedOutput(); err == nil {
		t.Errorf("worktree made from where the base is now, not the checked commit %s: %s", checked, out)
	}
	if judged[0].Rev != checked {
		t.Errorf("judged %+v, want %s", judged[0], checked)
	}

	judged = nil
	e = f.engine()
	e.Gate = func(_ context.Context, _ string, at StartPoint) error { judged = append(judged, at); return nil }
	if _, err := e.Run(ctx, Request{Change: "old", Mode: Start}); err != nil {
		t.Fatal(err)
	}
	if !judged[0].Branch || judged[0].Ref != "old" || judged[0].Rev != checked {
		t.Errorf("retained branch judged as %+v", judged[0])
	}
	// A branch that is not at the pinned commit is refused.
	testenv.Git(t, f.dir, "branch", "moved", "main")
	cfg := f.cfg.Worktree
	cfg.Branch = "moved"
	_, err := f.repo.EnsureWorktreeFrom(ctx, cfg, "moved", &StartPoint{Rev: checked, Ref: "moved", Branch: true}, nil)
	if err == nil || !strings.Contains(err.Error(), "moved since") {
		t.Errorf("pinned to a moved branch: %v", err)
	}
}
