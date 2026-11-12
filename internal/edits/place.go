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

// Locate finds where change is edited, for the repository containing dir.
// The change's worktree is the worktree of the repository whose run state
// is for change. The change folder must exist there.
func Locate(ctx context.Context, dir, change string) (Place, error) {
	if err := statedir.CheckID("change", change); err != nil {
		return Place{}, err
	}
	repo, err := runs.OpenRepo(ctx, dir)
	if err != nil {
		return Place{}, err
	}
	p := Place{Change: change, Dir: repo.Root}
	if wts, err := repo.Worktrees(ctx); err == nil {
		for _, wt := range wts {
			st, err := statedir.Resolve(ctx, wt)
			if err != nil {
				continue
			}
			if r, err := runs.ReadRun(st.RunFile()); err == nil && r.Change == change {
				p = Place{Change: change, Dir: wt, Worktree: true, State: st}
				break
			}
		}
	}
	if fi, err := os.Stat(p.ChangeDir()); err != nil || !fi.IsDir() {
		return p, fmt.Errorf("no change %s in %s (no folder openspec/changes/%s)", change, p.Describe(), change)
	}
	return p, nil
}
