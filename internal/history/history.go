// Package history keeps a run's event history: an append-only JSON Lines
// file, history.jsonl, in the worktree's state directory next to run.json.
// run.json is the current state; history.jsonl is the timeline of how the
// run got there.
//
// # File format
//
// The file is at File(<state dir>), that is
// $(git rev-parse --git-dir)/tuckerbox/history.jsonl. Each line is one
// Event as a JSON object, oldest first, ended by "\n". Only the run's
// engine process appends to it, while it holds the change lock, so there
// is one writer at a time. Lines are never rewritten or removed; the file
// goes when the worktree goes.
//
// A process that dies mid-write can leave a torn last line (no "\n", or
// half a JSON object). Read skips it. Before its next append, Append ends
// such a line with "\n" so the new event starts on a line of its own; Read
// then skips the torn line wherever it is.
//
// Every line has "v" (Version). Fields may be added within a version,
// never renamed or removed, and readers must ignore fields and kinds they
// don't know. A line looks like:
//
//	{"v":1,"time":"2026-11-12T09:00:00.123+10:00","kind":"rejected","change":"fix-login","step":"review","round":1,"agent":"codex","machine":"build-01","to":"build","summary":"not yet","findings":[{"severity":"high","file":"login.go","detail":"password is logged"}]}
//
// # Events
//
// Every event has time (RFC 3339 with the writer's offset), kind, change,
// round and machine; step and agent are set whenever the run has a
// current step. The kinds, and the fields each adds:
//
//   - started: a new run of the change began (first ever run).
//   - resumed: a stopped or blocked run was continued (tuckerbox resume, or
//     tuckerbox run on a run that isn't ready). step is where it continues.
//   - rerun: the run starts again from step (tuckerbox rerun).
//   - queued: the run joined the queue for a slot. Every started, resumed
//     and rerun is followed by queued, then by step_started once a slot is
//     free (or by stopped or failed).
//   - step_started: step's agent was started for round.
//   - step_finished: the step's agent reported a result: status (done,
//     rejected, blocked or failed), summary, findings and exit_code. A step
//     that ends without a result has no step_finished; a failed or stopped
//     event follows instead.
//   - handoff: step was done and the run moves on to the step in to;
//     summary is the finished step's summary.
//   - rejected: step rejected the change in round, with summary and
//     findings. to is the step the run goes back to (its on_reject); it is
//     empty when the run blocks instead (no on_reject, or max_rounds
//     reached), and a blocked event follows.
//   - blocked, failed, stopped: the run paused or ended there, with reason.
//     A stopped event whose reason is runs.Crashed was written on the next
//     resume or rerun for a run whose process died without recording a
//     final state; its time is when that was found, not when it died.
//   - ready: the last step was done and the run's work is on its branch.
//
// After a rejection that loops back, the next event is step_started for
// the to step in the next round.
package history

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// Version is the format version of each line, its "v" field.
const Version = 1

// FileName is the history file's name in a state directory.
const FileName = "history.jsonl"

// File is the history file of a state directory.
func File(d statedir.Dir) string { return filepath.Join(string(d), FileName) }

// Kind is what an event records.
type Kind string

// Event kinds; see the package documentation for what each carries.
const (
	Started      Kind = "started"
	Resumed      Kind = "resumed"
	Rerun        Kind = "rerun"
	Queued       Kind = "queued"
	StepStarted  Kind = "step_started"
	StepFinished Kind = "step_finished"
	Handoff      Kind = "handoff"
	Rejected     Kind = "rejected"
	Blocked      Kind = "blocked"
	Failed       Kind = "failed"
	Stopped      Kind = "stopped"
	Ready        Kind = "ready"
)

// Event is one line of history.jsonl.
type Event struct {
	// V is the line's format version (Version); Append sets it.
	V int `json:"v"`
	// Time is when the event happened, with the writer's offset.
	Time   time.Time `json:"time"`
	Kind   Kind      `json:"kind"`
	Change string    `json:"change"`
	// Step is the run's current step, or "" when it has none (ready).
	Step string `json:"step,omitempty"`
	// Round is the run's round when the event happened.
	Round int `json:"round"`
	// Agent is the agent configured for Step.
	Agent string `json:"agent,omitempty"`
	// Machine is the host name of the machine the run is on.
	Machine string `json:"machine,omitempty"`
	// To is the next step, for handoff and a looping rejected.
	To string `json:"to,omitempty"`
	// Status is the step's result, for step_finished.
	Status agent.Status `json:"status,omitempty"`
	// Summary is the step's summary, for step_finished, handoff and
	// rejected.
	Summary string `json:"summary,omitempty"`
	// Findings are the step's findings, for step_finished and rejected.
	Findings []agent.Finding `json:"findings,omitempty"`
	// ExitCode is the agent's exit status, for step_finished.
	ExitCode *int `json:"exit_code,omitempty"`
	// Reason says why, for blocked, failed and stopped.
	Reason string `json:"reason,omitempty"`
}

// Machine returns this machine's name for Event.Machine: its host name,
// or "" if it has none.
func Machine() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

// Append adds ev as one line at the end of the history file at path,
// creating the file and its directory if needed. A torn last line left by
// a crashed writer is ended first, so ev is always a line of its own. Only
// the run's engine, holding the change lock, may call it.
func Append(path string, ev Event) error {
	ev.V = Version
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if fi, err := f.Stat(); err == nil && fi.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, fi.Size()-1); err == nil && last[0] != '\n' {
			line = append([]byte{'\n'}, line...)
		}
	}
	// One write per event, so a crash tears at most this line.
	if _, err := f.Write(line); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Read returns the events in the history file at path, oldest first. A
// missing file is an empty history (nil, nil). Lines that don't parse as
// an event are torn writes and are skipped.
func Read(path string) ([]Event, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads history lines from r, as Read does.
func Parse(r io.Reader) ([]Event, error) {
	var out []Event
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		// A last line without "\n" may still be being written, or torn:
		// use it only if it is a whole event.
		if line = bytes.TrimSpace(line); len(line) > 0 {
			var ev Event
			if json.Unmarshal(line, &ev) == nil && ev.Kind != "" {
				out = append(out, ev)
			}
		}
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
	}
}
