// Package runs takes OpenSpec changes through a repository's workflow, one
// process per run: it picks changes, makes their worktrees, holds the
// change and slot locks, runs each step's agent, and records run state.
//
// Concurrency follows formal/runs.tla; see "Locks and the model" in
// openspec/changes/add-core-runner/design.md for how each action and
// invariant maps to this package.
package runs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/scope"
)

// State is a run's state.
type State string

// Run states.
const (
	Queued  State = "queued"
	Running State = "running"
	Blocked State = "blocked"
	Stopped State = "stopped"
	Failed  State = "failed"
	Ready   State = "ready"
)

// RunVersion is the format version of run.json.
const RunVersion = 1

// Run is the content of run.json. Only the run's engine process writes it,
// while it holds the change lock.
type Run struct {
	Version int    `json:"version"`
	Change  string `json:"change"`
	// Repo is the checkout the run was started from; its config is used.
	Repo     string `json:"repo"`
	Worktree string `json:"worktree"`
	Branch   string `json:"branch"`
	Base     string `json:"base,omitempty"`
	State    State  `json:"state"`
	// Step is the current step (the next to run when not running), or ""
	// when the run is ready.
	Step string `json:"step"`
	// Round increases whenever a step starts again: after a rejection, a
	// resume or a rerun. It never decreases, so a result from an earlier
	// attempt can never match it (StaleNeverAccepted).
	Round int `json:"round"`
	// Reason says why the run is blocked, failed or stopped.
	Reason string `json:"reason,omitempty"`
	// Tail is the end of the step's output when it failed without a result.
	Tail string `json:"tail,omitempty"`
	// Rejections counts each step's rejections since the last resume or
	// rerun, for max_rounds.
	Rejections map[string]int `json:"rejections,omitempty"`
	// Scope is the run's task scope, or nil for the whole change. resume
	// and rerun keep it; run with another scope starts the run again.
	Scope *scope.Scope `json:"scope,omitempty"`
	// Complete records, in the write that makes the run ready, whether
	// every task of the change other than ship tasks was ticked. Any
	// write that leaves ready clears it. Dependents read it from this one
	// file, never from tasks.md (formal/scope.tla).
	Complete bool `json:"complete"`
	// History holds every accepted step result, oldest first.
	History []Entry `json:"history,omitempty"`
	// Pid is the engine process, AgentPid the running step's agent.
	Pid      int `json:"pid,omitempty"`
	AgentPid int `json:"agent_pid,omitempty"`
	// Since is when the state, step or round last changed.
	Since time.Time `json:"since"`
	// Created is when the run was first started.
	Created time.Time `json:"created"`
}

// Entry is one finished step.
type Entry struct {
	Step     string          `json:"step"`
	Round    int             `json:"round"`
	Status   agent.Status    `json:"status"`
	Summary  string          `json:"summary,omitempty"`
	Findings []agent.Finding `json:"findings,omitempty"`
	ExitCode int             `json:"exit_code"`
	Ended    time.Time       `json:"ended"`
}

// ErrNoRun means there is no run.json.
var ErrNoRun = errors.New("no run recorded")

// ReadRun reads run.json at p.
func ReadRun(p string) (*Run, error) {
	var data []byte
	var err error
	// On Windows a read can race the engine's rename; retry briefly.
	for i := 0; i < 20; i++ {
		data, err = os.ReadFile(p)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoRun
	}
	if err != nil {
		return nil, err
	}
	var r Run
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return &r, nil
}

// WriteRun writes r to p atomically: a temporary file in the same directory,
// then a rename over p, so a reader sees the old or the new file, never half.
func WriteRun(p string, r *Run) error {
	r.Version = RunVersion
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(p, append(data, '\n'))
}

func writeAtomic(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp-" + strconv.Itoa(os.Getpid())
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	for i := 0; ; i++ {
		err = os.Rename(tmp, p)
		// On Windows the rename fails while a reader has p open; retry.
		if err == nil || runtime.GOOS != "windows" || i == 50 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// Earlier returns the rejected results in the history, oldest first: the
// findings a step re-run after a rejection must address.
func (r *Run) Earlier() []agent.Result {
	var out []agent.Result
	for _, e := range r.History {
		if e.Status == agent.Rejected {
			out = append(out, agent.Result{
				Change: r.Change, Step: e.Step, Round: e.Round, Status: e.Status,
				Summary: e.Summary, Findings: e.Findings,
			})
		}
	}
	return out
}

// Crashed is the reason shown for a run whose process ended while the run
// was queued or running, without recording a final state.
const Crashed = "the run process ended without recording a final state"

// Effective returns the run as people should see it: a queued or running
// run whose change lock is free has no process any more and is shown as
// stopped. The file itself is left alone; only the engine writes it.
func (r *Run) Effective(active bool) Run {
	v := *r
	if !active && (v.State == Queued || v.State == Running) {
		v.State = Stopped
		v.Reason = Crashed
		v.Pid, v.AgentPid = 0, 0
	}
	return v
}
