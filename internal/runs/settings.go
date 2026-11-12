package runs

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/deps"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/settings"
)

// OverridesError means a change's own step settings (its tuckerbox.toml)
// are not allowed by the repository config the run uses, so the run was
// refused before any worktree or run state changed.
type OverridesError struct {
	Change string
	// Where is what was judged: "in its worktree <path>", or "at <ref>
	// (<commit>)" for a new worktree.
	Where    string
	Problems []settings.Problem
}

func (e *OverridesError) Error() string {
	var b strings.Builder
	b.WriteString("change " + e.Change + ": its step overrides are not allowed " + e.Where + "; fix " + sidecarRel(e.Change) + ", or allow the values under [overrides] in .tuckerbox/config.toml:")
	for _, p := range e.Problems {
		b.WriteString("\n  ")
		b.WriteString(p.String())
	}
	return b.String()
}

// sidecarRel is a change's sidecar relative to the repository root.
func sidecarRel(change string) string {
	return path.Join("openspec", "changes", change, settings.FileName)
}

// readSidecar returns change's tuckerbox.toml where sp says the run starts
// from: the worktree's file, else the file in commit sp.Rev. nil means the
// change has none there.
func readSidecar(ctx context.Context, root, change string, sp StartPoint) ([]byte, error) {
	if sp.Worktree != "" {
		data, err := os.ReadFile(filepath.Join(deps.ChangesDir(sp.Worktree), change, settings.FileName))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return data, err
	}
	rel := sidecarRel(change)
	if out, err := git(ctx, root, "ls-tree", "--name-only", sp.Rev, "--", rel); err != nil {
		return nil, err
	} else if out == "" {
		return nil, nil
	}
	data, err := git(ctx, root, "cat-file", "blob", sp.Rev+":"+rel)
	if err != nil {
		return nil, err
	}
	return []byte(data + "\n"), nil
}

// CheckOverrides refuses change (with an *OverridesError) when its step
// overrides, read where sp says the run starts from, are not allowed by
// cfg: the same files the approval run gate judges. root is the
// repository checkout, for reading a commit.
func CheckOverrides(ctx context.Context, cfg *config.Config, root, change string, sp StartPoint) error {
	data, err := readSidecar(ctx, root, change, sp)
	if err != nil {
		return err
	}
	if _, ps := settings.Judge(cfg, data); len(ps) > 0 {
		where := "in its worktree " + sp.Worktree
		if sp.Worktree == "" {
			where = "at " + sp.Ref + " (" + shortRev(sp.Rev) + ")"
		}
		return &OverridesError{Change: change, Where: where, Problems: ps}
	}
	return nil
}

func shortRev(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
}

// stepSettings works out what step st runs with in the worktree ws: the
// change's overrides, read now, over the config's step. It returns the
// step to run and what to record, or a reason the step may not start.
func (e *Engine) stepSettings(ctx context.Context, ws Workspace, change string, st config.Step) (config.Step, *history.Settings, string) {
	data, err := readSidecar(ctx, e.Repo.Root, change, StartPoint{Worktree: ws.Path})
	if err != nil {
		return st, nil, "reading the change's settings: " + err.Error()
	}
	sc, all := settings.Judge(e.Config, data)
	// Only what stops this step; the run's start judged the rest.
	var ps []settings.Problem
	for _, p := range all {
		if p.Key == "" || p.Key == "steps."+st.ID || strings.HasPrefix(p.Key, "steps."+st.ID+".") {
			ps = append(ps, p)
		}
	}
	if len(ps) > 0 {
		oe := &OverridesError{Change: change, Where: "in its worktree " + ws.Path, Problems: ps}
		return st, nil, "step " + st.ID + " not started: " + oe.Error()
	}
	eff := settings.Resolve(e.Config, st, sc.Steps[st.ID])
	run := eff.Apply(st)
	if run.Permissions != "" {
		// As for a repository step: only a profile this machine defines,
		// for this remote and agent.
		if msg := e.Config.ProfileProblem(run.Permissions, run.Agent); msg != "" {
			return st, nil, "step " + st.ID + " not started: " + msg
		}
	}
	return run, recordSettings(eff), ""
}

// recordSettings is what history and run.json record of eff: names and
// sources, never a profile's flags.
func recordSettings(eff settings.Effective) *history.Settings {
	return &history.Settings{
		Agent:       history.Setting{Value: eff.Agent, Source: string(eff.From.Agent)},
		Model:       history.Setting{Value: eff.Model, Source: string(eff.From.Model)},
		Effort:      history.Setting{Value: eff.Effort, Source: string(eff.From.Effort)},
		Permissions: history.Setting{Value: eff.Permissions, Source: string(eff.From.Permissions)},
	}
}

// plannedAgent is the agent step would start with now in the worktree ws:
// the change's override if it has a valid one, else the config's.
func (e *Engine) plannedAgent(ws Workspace, change, step string) string {
	st, ok := e.Config.Step(step)
	if !ok {
		return ""
	}
	data, err := readSidecar(context.Background(), e.Repo.Root, change, StartPoint{Worktree: ws.Path})
	if err != nil {
		return st.Agent
	}
	sc, ps := settings.Judge(e.Config, data)
	if len(ps) > 0 {
		return st.Agent
	}
	return settings.Resolve(e.Config, st, sc.Steps[step]).Agent
}
