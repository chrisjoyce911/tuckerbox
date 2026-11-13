package runs

import (
	"context"
	"errors"
	"os"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
)

// StartedMode is the operating mode the run started in. A run started
// before modes existed records none and is solo. Changing the
// repository's mode never changes a run already started
// (operating-modes: Changing mode): every gate and behaviour of an
// existing run uses this, never the current config.
func (r *Run) StartedMode() config.Mode {
	if r == nil || r.Mode == "" {
		return config.ModeSolo
	}
	return r.Mode
}

// EffectiveMode is the mode that governs change's run: the mode an existing
// run started in, else, for a new start, the repository's current mode.
func EffectiveMode(cfg *config.Config, existing *Run) config.Mode {
	if existing != nil {
		return existing.StartedMode()
	}
	if cfg == nil || cfg.Mode == "" {
		return config.ModeSolo
	}
	return cfg.Mode
}

// SavedRun returns change's run on this machine, found at its worktree
// under cfg's pattern, without making anything; nil with no error when
// there is no worktree or no run.
func (r Repo) SavedRun(ctx context.Context, cfg config.Worktree, change string) (*Run, error) {
	path := r.WorktreePath(cfg, change)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	wts, err := r.Worktrees(ctx)
	if err != nil {
		return nil, err
	}
	found := false
	for _, w := range wts {
		if samePath(w, path) {
			found = true
			break
		}
	}
	if !found {
		return nil, nil
	}
	return savedRunAt(ctx, path)
}

func savedRunAt(ctx context.Context, worktree string) (*Run, error) {
	ws, err := OpenWorkspace(ctx, worktree)
	if err != nil {
		return nil, err
	}
	rs, err := ReadRun(ws.State.RunFile())
	if errors.Is(err, ErrNoRun) {
		return nil, nil
	}
	return rs, err
}

// freezeMode makes the engine run req in the mode its existing run started
// in, before any gate, slot or claim: e.Config becomes a copy with that
// mode. A new run keeps the repository's mode.
func (e *Engine) freezeMode(ctx context.Context, req Request) error {
	var existing *Run
	var err error
	if req.Worktree != "" {
		existing, err = savedRunAt(ctx, req.Worktree)
	} else {
		existing, err = e.Repo.SavedRun(ctx, e.Config.Worktree, req.Change)
	}
	if err != nil {
		return err
	}
	if mode := EffectiveMode(e.Config, existing); mode != e.Config.Mode {
		cfg := *e.Config
		cfg.Mode = mode
		e.Config = &cfg
	}
	return nil
}
