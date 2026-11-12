// Package edits changes an OpenSpec change's files for a person: opening one
// in their editor, ticking a task, validating strictly. It decides where
// those edits happen and keeps them out of the way of a run by holding the
// change's run lock (see [WithChangeLock]).
//
// None of it commits. The person commits and pushes, as with any edit.
package edits

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// Place is where a change's files are edited on this machine: the change's
// worktree when it has one here, otherwise the repository checkout the
// command runs in.
type Place struct {
	// Change is the change id.
	Change string
	// Dir is the folder edits happen in: a worktree or checkout root.
	Dir string
	// Worktree is true when Dir is the change's own worktree.
	Worktree bool
	// State is the worktree's Tuckerbox state directory, which holds the
	// change lock; "" for the checkout, where no run can be working (runs
	// always work in their own worktree), so there is no lock to take.
	State statedir.Dir
}

// ChangeDir is the change's folder, openspec/changes/<id>, in Dir.
func (p Place) ChangeDir() string {
	return filepath.Join(p.Dir, "openspec", "changes", p.Change)
}

// Describe names the folder for people: "the change's worktree <dir>" or
// "the repository checkout <dir>".
func (p Place) Describe() string {
	if p.Worktree {
		return "the change's worktree " + p.Dir
	}
	return "the repository checkout " + p.Dir
}

// Locate finds where change is edited, for the repository containing dir,
// with the worktree settings of that repository's config (the defaults when
// it has none). See [LocateWith].
func Locate(ctx context.Context, dir, change string) (Place, error) {
	var wt config.Worktree
	env := config.SystemEnv()
	if opts, err := config.Discover(env, dir); err == nil {
		// A config with problems still names its worktree settings; a run
		// would refuse it anyway.
		if cfg, _ := config.Load(opts); cfg != nil {
			wt = cfg.Worktree
		}
	}
	return LocateWith(ctx, dir, change, wt)
}

// LocateWith finds where change is edited, for the repository containing
// dir. The change's worktree is the repository's worktree that:
//
//   - holds run state for change, or
//   - is at the path wt gives change's runs, where a run makes or reuses
//     its worktree (runs.Repo.EnsureWorktree),
//
// whether or not a run has recorded state there yet: a first run holds the
// change lock before it writes any. Otherwise it is the checkout. The
// change folder must exist in the place found.
func LocateWith(ctx context.Context, dir, change string, wt config.Worktree) (Place, error) {
	if err := statedir.CheckID("change", change); err != nil {
		return Place{}, err
	}
	repo, err := runs.OpenRepo(ctx, dir)
	if err != nil {
		return Place{}, err
	}
	p := Place{Change: change, Dir: repo.Root}
	wts, err := repo.Worktrees(ctx)
	if err != nil {
		return p, err
	}
	found := ""
	for _, w := range wts {
		st, err := statedir.Resolve(ctx, w)
		if err != nil {
			continue
		}
		if r, err := runs.ReadRun(st.RunFile()); err == nil && r.Change == change {
			found = w
			break
		}
	}
	if found == "" {
		path := repo.WorktreePath(wt, change)
		for i, w := range wts {
			if i > 0 && samePath(w, path) { // never the main worktree
				found = w
				break
			}
		}
	}
	if found != "" {
		st, err := statedir.Resolve(ctx, found)
		if err != nil {
			return p, err
		}
		p = Place{Change: change, Dir: found, Worktree: true, State: st}
	}
	if fi, err := os.Stat(p.ChangeDir()); err != nil || !fi.IsDir() {
		return p, fmt.Errorf("no change %s in %s (no folder openspec/changes/%s)", change, p.Describe(), change)
	}
	return p, nil
}

func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(fa, fb)
}
