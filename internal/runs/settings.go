package runs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
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

// readAt returns the file rel (relative to the repository root) where sp
// says the run starts from: the worktree's file, else the file in commit
// sp.Rev. nil means there is none there.
func readAt(ctx context.Context, root, rel string, sp StartPoint) ([]byte, error) {
	if sp.Worktree != "" {
		data, err := os.ReadFile(filepath.Join(sp.Worktree, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return data, err
	}
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

// readSidecar returns change's tuckerbox.toml where sp says the run starts
// from. nil means the change has none there.
func readSidecar(ctx context.Context, root, change string, sp StartPoint) ([]byte, error) {
	return readAt(ctx, root, sidecarRel(change), sp)
}

// where says what sp is, for messages.
func where(sp StartPoint) string {
	if sp.Worktree != "" {
		return "in its worktree " + sp.Worktree
	}
	return "at " + sp.Ref + " (" + shortRev(sp.Rev) + ")"
}

// startConfig is cfg with the [overrides] allow-list read from the
// repository config where sp says the run starts from, the same files the
// run gate judges, rather than from the checkout cfg was loaded from.
// Everything else, machine profiles and their remote grants included,
// stays as cfg has it. A start point without a repository config allows
// no overrides.
func startConfig(ctx context.Context, cfg *config.Config, root string, sp StartPoint) (*config.Config, error) {
	rel := filepath.ToSlash(config.RepoFile)
	data, err := readAt(ctx, root, rel, sp)
	if err != nil {
		return nil, err
	}
	c := *cfg
	c.Overrides = nil
	if data == nil {
		return &c, nil
	}
	o, ps := config.RepoOverrides(rel, data)
	if len(ps) > 0 {
		return nil, fmt.Errorf("repository config %s: %w", where(sp), ps)
	}
	c.Overrides = o
	return &c, nil
}

// CheckOverrides refuses change (with an *OverridesError) when its step
// overrides are not allowed by the [overrides] allow-list, both read
// where sp says the run starts from: the same files the approval run gate
// judges. cfg supplies everything else, such as machine profiles. root is
// the repository checkout, for reading a commit.
func CheckOverrides(ctx context.Context, cfg *config.Config, root, change string, sp StartPoint) error {
	scfg, err := startConfig(ctx, cfg, root, sp)
	if err != nil {
		return err
	}
	data, err := readSidecar(ctx, root, change, sp)
	if err != nil {
		return err
	}
	if _, ps := settings.Judge(scfg, data); len(ps) > 0 {
		return &OverridesError{Change: change, Where: where(sp), Problems: ps}
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
	sp := StartPoint{Worktree: ws.Path}
	cfg, err := startConfig(ctx, e.Config, e.Repo.Root, sp)
	if err != nil {
		return st, nil, "step " + st.ID + " not started: " + err.Error()
	}
	data, err := readSidecar(ctx, e.Repo.Root, change, sp)
	if err != nil {
		return st, nil, "reading the change's settings: " + err.Error()
	}
	sc, all := settings.Judge(cfg, data)
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
	eff := settings.Resolve(cfg, st, sc.Steps[st.ID])
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
	sp := StartPoint{Worktree: ws.Path}
	cfg, err := startConfig(context.Background(), e.Config, e.Repo.Root, sp)
	if err != nil {
		return st.Agent
	}
	data, err := readSidecar(context.Background(), e.Repo.Root, change, sp)
	if err != nil {
		return st.Agent
	}
	sc, ps := settings.Judge(cfg, data)
	if len(ps) > 0 {
		return st.Agent
	}
	return settings.Resolve(e.Config, st, sc.Steps[step]).Agent
}
