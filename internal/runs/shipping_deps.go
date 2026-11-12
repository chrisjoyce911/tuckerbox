package runs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/deps"
)

type shippingDeps struct {
	at       time.Time
	base     string
	archives map[string]string
	active   map[string]bool
}

func (w *depWait) failureState() State {
	if w != nil && w.blocked {
		return Blocked
	}
	return Failed
}
func (w *depWait) shippingPending(ctx context.Context) ([]deps.Unmet, []string, string, error) {
	refresh := w.e.DependencyRefresh
	if refresh <= 0 {
		refresh = 30 * time.Second
	}
	if w.shipping == nil || w.e.now().Sub(w.shipping.at) >= refresh {
		ref, err := w.e.Repo.Base(ctx, w.e.Config.Worktree.Base, func(s string) { w.e.logf(w.rs.Change, "%s", s) })
		if err != nil {
			return nil, nil, "", err
		}
		base, err := git(ctx, w.e.Repo.Root, "rev-parse", ref+"^{commit}")
		if err != nil {
			return nil, nil, "", err
		}
		tree, err := git(ctx, w.e.Repo.Root, "ls-tree", "-r", "--name-only", base, "--", "openspec/changes/")
		if err != nil {
			return nil, nil, "", err
		}
		snapshot := &shippingDeps{at: w.e.now(), base: base, archives: map[string]string{}, active: map[string]bool{}}
		for _, path := range strings.Split(tree, "\n") {
			rest := strings.TrimPrefix(path, "openspec/changes/")
			parts := strings.Split(rest, "/")
			if len(parts) >= 2 && parts[0] != "archive" {
				snapshot.active[parts[0]] = true
			}
			if len(parts) >= 3 && parts[0] == "archive" && len(parts[1]) > 11 && parts[1][10] == '-' && parts[2] == "proposal.md" {
				if _, err := time.Parse("2006-01-02", parts[1][:10]); err == nil {
					snapshot.archives[parts[1][11:]] = "openspec/changes/archive/" + parts[1] + "/proposal.md"
				}
			}
		}
		w.shipping = snapshot
	}
	var unmet []deps.Unmet
	var unknown []string
	for _, id := range w.ids {
		if w.shipping.archives[id] != "" {
			continue
		}
		known := w.shipping.active[id]
		for _, root := range w.roots {
			known = known || deps.IsActive(root, id) || deps.IsArchived(root, id)
		}
		if !known {
			unknown = append(unknown, id)
			continue
		}
		state, step := w.l.lookup(id)
		why := state
		if why == "" {
			why = "not started"
		}
		if state == "running" && step != "" {
			why = step
		}
		unmet = append(unmet, deps.Unmet{ID: id, Why: why})
	}
	if len(unmet) > 0 || len(unknown) > 0 {
		return unmet, unknown, "", nil
	}
	// A worktree may have been made while its dependencies were still waiting.
	// Include their base commits before any agent starts, preserving local work.
	needsUpdate := false
	for _, id := range w.ids {
		intro, err := git(ctx, w.ws.Path, "log", "-1", "--format=%H", w.shipping.base, "--", w.shipping.archives[id])
		if err != nil {
			return nil, nil, "", err
		}
		if intro != "" {
			if _, err := git(ctx, w.ws.Path, "merge-base", "--is-ancestor", intro, "HEAD"); err != nil {
				needsUpdate = true
			}
		}
	}
	if needsUpdate {
		status, err := git(ctx, w.ws.Path, "status", "--porcelain", "--untracked-files=all")
		if err != nil {
			return nil, nil, "", err
		}
		if status != "" {
			return nil, nil, "dependency base is newer; worktree is not clean", nil
		}
		if _, err := git(ctx, w.ws.Path, "merge", "--no-edit", w.shipping.base); err != nil {
			files, _ := git(ctx, w.ws.Path, "diff", "--name-only", "--diff-filter=U")
			if _, abortErr := git(ctx, w.ws.Path, "merge", "--abort"); abortErr != nil {
				return nil, nil, "", abortErr
			}
			return nil, nil, fmt.Sprintf("conflict updating dependency base: %s", strings.ReplaceAll(files, "\n", ", ")), nil
		}
		if w.e.Gate != nil {
			if err := w.e.Gate(ctx, w.rs.Change, StartPoint{Worktree: w.ws.Path}); err != nil {
				return nil, nil, err.Error(), nil
			}
		}
	}
	return nil, nil, "", nil
}
