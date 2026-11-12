package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/autopilot"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/priority"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
)

// autopilotWatch is how often autopilot reads the repository's run states
// to notice a run ending between full passes.
var autopilotWatch = 5 * time.Second

// autopilot: tuckerbox autopilot [--once] [--dry-run]
func (a App) autopilot(args []string) int {
	var once, dry bool
	pos, err := parseArgs(args, map[string]*bool{"once": &once, "dry-run": &dry}, nil)
	if err != nil || len(pos) > 0 {
		return a.usageErr("autopilot", "usage: tuckerbox autopilot [--once] [--dry-run]")
	}
	_, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		return a.errf("autopilot", "%v", err)
	}
	repo, cfg, ok := a.loadRepo("autopilot", wd)
	if !ok {
		return 1
	}
	if !cfg.RequireApproval {
		return a.errf("autopilot", "autopilot needs approval to be required, and this repository sets require_approval = false; approval is the one gate it keeps")
	}
	lockPath, markerPath := autopilot.Files(repo.Common)
	lock, err := runs.TryChangeLock(lockPath)
	if errors.Is(err, runs.ErrLocked) {
		m := autopilot.ReadMarker(repo.Common)
		if m != nil && m.Pid > 0 {
			return a.errf("autopilot", "autopilot is already running for this repository (pid %d on %s, since %s)", m.Pid, m.Machine, displayTime(m.Started))
		}
		return a.errf("autopilot", "autopilot is already running for this repository")
	}
	if err != nil {
		return a.errf("autopilot", "%v", err)
	}
	defer lock.Release()
	data, _ := json.Marshal(autopilot.Marker{Pid: os.Getpid(), Machine: history.Machine(), Started: time.Now()})
	if err := os.WriteFile(markerPath, data, 0o644); err != nil {
		return a.errf("autopilot", "%v", err)
	}
	defer os.Remove(markerPath)

	p := &autopilot.Pilot{
		Repo: repo, Config: cfg, Out: a.Stdout, DryRun: dry,
		Warn: func(msg string) { fmt.Fprintf(a.Stderr, "tuckerbox autopilot: warning: %s\n", msg) },
		Approval: func(ctx context.Context, change, base string) (approval.State, string, error) {
			return approvalAt(ctx, repo, cfg, change, base)
		},
		Start: func(_ context.Context, req autopilot.Request) error {
			return a.spawnQuiet(repo.Root, req.Args())
		},
		Order: func(_ context.Context, ids []string) ([]string, error) {
			return admissionOrder(repo, ids)
		},
	}
	ctx, stop := signal.NotifyContext(context.Background(), stopSignals...)
	defer stop()
	// prev is the run states before the last pass, or as last seen, so a
	// run the pass started that ends before the next read still counts as
	// ending.
	prev := runStates(ctx, repo)
	if _, err := p.Pass(ctx); err != nil {
		fmt.Fprintf(a.Stderr, "tuckerbox autopilot: %v\n", err)
		if once {
			return 1
		}
	}
	if once {
		return 0
	}
	fmt.Fprintf(a.Stdout, "autopilot: watching %s (a pass every %s; Ctrl-C stops autopilot, not the runs)\n", repo.Root, cfg.Autopilot.Interval)
	last := time.Now()
	t := time.NewTicker(autopilotWatch)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(a.Stdout, "autopilot: stopped; runs keep going")
			return 0
		case <-t.C:
		}
		now := runStates(ctx, repo)
		ended := false
		for id, st := range now {
			if old, ok := prev[id]; (!ok || old.state != st.state || !old.since.Equal(st.since)) && !st.busy {
				ended = true
			}
		}
		if !ended && time.Since(last) < cfg.Autopilot.Interval {
			prev = now
			continue
		}
		last = time.Now()
		prev = now
		if _, err := p.Pass(ctx); err != nil && ctx.Err() == nil {
			fmt.Fprintf(a.Stderr, "tuckerbox autopilot: %v\n", err)
		}
	}
}

// runState is what autopilot's watch compares between reads.
type runState struct {
	state runs.State
	since time.Time
	busy  bool
}

// runStates reads the repository's run states on this machine.
func runStates(ctx context.Context, repo runs.Repo) map[string]runState {
	out := map[string]runState{}
	local, _ := runs.LocalRuns(ctx, repo)
	for id, l := range local {
		out[id] = runState{state: l.Run.State, since: l.Run.Since, busy: l.Busy()}
	}
	return out
}

// spawnQuiet starts a detached engine like spawn, without printing the
// engine's own start line (autopilot prints its own).
func (a App) spawnQuiet(dir string, engineArgs []string) error {
	quiet := a
	quiet.Stdout = discard{}
	return quiet.spawn(dir, engineArgs)
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// approvalAt judges change's approval where tuckerbox run would start it:
// its worktree if it has one, else its branch or base. It returns the
// commit judged ("" for a worktree), which pins a new worktree.
func approvalAt(ctx context.Context, repo runs.Repo, cfg *config.Config, change, base string) (approval.State, string, error) {
	sp, err := repo.ResolveStart(ctx, cfg.Worktree, change, base, nil)
	if err != nil {
		return approval.None, "", err
	}
	// Approvals are published on the approvals ref (approval-publish); an
	// entry there wins over the sidecar, as in the run gate.
	entry, err := refEntry(ctx, repo.Root, change)
	if err != nil {
		return approval.None, "", err
	}
	if sp.Worktree != "" {
		st, err := approval.JudgeWith(approval.DirOf(sp.Worktree, change), entry)
		return st.State, "", err
	}
	rev, err := approval.NewRev(ctx, repo.Root, sp.Rev, change)
	if err != nil {
		return approval.None, "", err
	}
	if !rev.Exists() {
		return approval.None, sp.Rev, nil
	}
	st, err := approval.JudgeWith(rev, entry)
	return st.State, sp.Rev, err
}

// admissionOrder puts ids in add-priority's admission order (priority,
// inherited from dependents, then id), read under the sidecar lock as the
// slot queue reads it.
func admissionOrder(repo runs.Repo, ids []string) ([]string, error) {
	reqs := make([]priority.Request, len(ids))
	for i, id := range ids {
		reqs[i] = priority.Request{ID: id}
	}
	var all []priority.Change
	err := runs.WithSidecarLock(repo.Common, func() error {
		var err error
		all, err = priority.Load(repo.Root)
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ids))
	for _, r := range priority.Admission(reqs, all) {
		out = append(out, r.ID)
	}
	return out, nil
}
