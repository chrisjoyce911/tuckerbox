package runs

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/deps"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// depLookup finds the runs of a repository's changes on this machine by
// looking in each of its worktrees. State directories are cached, so a
// poll costs one git call plus a read of each run.json.
type depLookup struct {
	repo   Repo
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

func (l *depLookup) lookup(id string) (string, string) {
	r, ok := l.runs[id]
	if !ok {
		return "", ""
	}
	return string(r.State), r.Step
}

// waitDeps holds the run, queued and without a slot or a queue ticket,
// until every dependency the change declares in its worktree is ready on
// this machine or archived (in the worktree or the checkout the run was
// started from). notify is called before the first wait, so tuckerbox run
// does not wait with it. It returns a non-empty fail reason when the run
// can never start, and ctx's error when it is stopped.
func (e *Engine) waitDeps(ctx context.Context, ws Workspace, rs *Run, notify func()) (fail string, err error) {
	ids, err := deps.Read(ws.Path, rs.Change)
	if err != nil {
		return "reading dependencies: " + err.Error(), nil
	}
	if len(ids) == 0 {
		return "", nil
	}
	roots := []string{ws.Path}
	if filepath.Clean(e.Repo.Root) != filepath.Clean(ws.Path) {
		roots = append(roots, e.Repo.Root)
	}
	l := newDepLookup(e.Repo)
	poll := 4 * e.poll()
	for {
		if err := l.refresh(ctx); err != nil && ctx.Err() == nil {
			e.logf(rs.Change, "warning: reading dependency runs: %v", err)
		}
		unmet, unknown := deps.Pending(roots, ids, l.lookup)
		if len(unknown) > 0 {
			return fmt.Sprintf("depends on %s, which is neither an active nor an archived change (see tuckerbox check)", strings.Join(unknown, ", ")), nil
		}
		reason := ""
		if len(unmet) > 0 {
			reason = deps.Reason(unmet)
		}
		if reason != rs.Reason {
			rs.Reason = reason
			if err := e.save(ws, rs); err != nil {
				return "", err
			}
			if reason != "" {
				e.logf(rs.Change, "queued: %s", reason)
			}
		}
		if len(unmet) == 0 {
			return "", nil
		}
		notify()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(poll):
		}
	}
}
