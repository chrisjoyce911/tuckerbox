package runs

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/deps"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// depLookup finds the runs of a repository's changes on this machine by
// looking in each of its worktrees. State directories are cached, so a
// poll costs one git call plus a read of each run.json.
type depLookup struct {
	repo Repo
	// archiveOnly is set outside ship = "local": there a dependency is met
	// only once archived, whatever its run says (task-scope: Partial runs
	// don't ship).
	archiveOnly bool
	states map[string]statedir.Dir // worktree -> state dir
	runs   map[string]Run          // change -> effective run, this poll
}

func newDepLookup(repo Repo) *depLookup {
	return &depLookup{repo: repo, states: map[string]statedir.Dir{}}
}

// refresh reads the run of every worktree.
func (l *depLookup) refresh(ctx context.Context) error {
	wts, err := l.repo.Worktrees(ctx)
	if err != nil {
		return err
	}
	l.runs = map[string]Run{}
	for _, wt := range wts {
		st, ok := l.states[wt]
		if !ok {
			st, err = statedir.Resolve(ctx, wt)
			if err != nil {
				continue // removed meanwhile
			}
			l.states[wt] = st
		}
		r, err := ReadRun(st.RunFile())
		if depReadHook != nil {
			depReadHook()
		}
		if err != nil {
			continue
		}
		v := r.Effective(Active(st.RunLock()))
		// A change has one worktree in practice; prefer a ready run if
		// it somehow has more.
		if old, ok := l.runs[v.Change]; ok && old.State == Ready {
			continue
		}
		l.runs[v.Change] = v
	}
	return nil
}

// lookup gives deps.Pending a dependency's state from the one read of its
// run.json in refresh, never from its tasks.md (formal/scope.tla,
// DependentSeesReviewedWork). A ready run counts as ready only when it has
// no scope or recorded the change as complete. A scoped run's scope is
// added to what the queued reason shows: "a (ready, tasks 1)".
func (l *depLookup) lookup(id string) (string, string) {
	r, ok := l.runs[id]
	if !ok {
		return "", ""
	}
	if l.archiveOnly && r.State == Ready {
		return string(r.State) + ", not archived", r.Step
	}
	if r.Scope == nil {
		return string(r.State), r.Step
	}
	tasks := ", tasks " + r.Scope.Spec
	switch {
	case r.State == Ready && r.Complete:
		return string(r.State), r.Step
	case r.State == Running && r.Step != "":
		return string(r.State), r.Step + tasks
	}
	return string(r.State) + tasks, r.Step
}

// depReadHook, when set by a test, runs right after each read of a
// dependency's run.json, so a test can move the dependency on between a
// dependent's reads.
var depReadHook func()

// depWait holds a run until its dependencies are met.
type depWait struct {
	e     *Engine
	ws    Workspace
	rs    *Run
	ids   []string
	roots []string
	l     *depLookup
}

// newDepWait reads the dependencies the change declares in its worktree.
// It returns a fail reason when they cannot be read.
func (e *Engine) newDepWait(ws Workspace, rs *Run) (*depWait, string) {
	ids, err := deps.Read(ws.Path, rs.Change)
	if err != nil {
		return nil, "reading dependencies: " + err.Error()
	}
	roots := []string{ws.Path}
	if filepath.Clean(e.Repo.Root) != filepath.Clean(ws.Path) {
		roots = append(roots, e.Repo.Root)
	}
	l := newDepLookup(e.Repo)
	l.archiveOnly = e.Config.Ship != "" && e.Config.Ship != config.ShipLocal
	return &depWait{e: e, ws: ws, rs: rs, ids: ids, roots: roots, l: l}, ""
}

// check looks once: every dependency must be ready on this machine or
// archived (in the worktree or the checkout the run was started from).
// It records the queued reason when it changed, and returns whether all
// are met, or a fail reason when the run can never start.
func (w *depWait) check(ctx context.Context) (met bool, fail string, err error) {
	if len(w.ids) == 0 {
		return true, "", nil
	}
	if err := w.l.refresh(ctx); err != nil && ctx.Err() == nil {
		w.e.logf(w.rs.Change, "warning: reading dependency runs: %v", err)
	}
	unmet, unknown := deps.Pending(w.roots, w.ids, w.l.lookup)
	if len(unknown) > 0 {
		return false, fmt.Sprintf("depends on %s, which is neither an active nor an archived change (see tuckerbox check)", strings.Join(unknown, ", ")), nil
	}
	reason := ""
	if len(unmet) > 0 {
		reason = deps.Reason(unmet)
	}
	if reason != w.rs.Reason {
		w.rs.Reason = reason
		if err := w.e.save(w.ws, w.rs); err != nil {
			return false, "", err
		}
		if reason != "" {
			w.e.logf(w.rs.Change, "queued: %s", reason)
		}
	}
	return len(unmet) == 0, "", nil
}

// wait holds the run, queued and without a slot or a queue ticket, until
// check passes. notify is called before the first wait, so tuckerbox run
// does not wait with it. It returns a non-empty fail reason when the run
// can never start, and ctx's error when it is stopped.
func (w *depWait) wait(ctx context.Context, notify func()) (fail string, err error) {
	poll := 4 * w.e.poll()
	for {
		met, fail, err := w.check(ctx)
		if met || fail != "" || err != nil {
			return fail, err
		}
		notify()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(poll):
		}
	}
}
