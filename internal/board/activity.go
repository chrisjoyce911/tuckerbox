package board

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/scope"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// Activity is what a card shows about its run's agent (agent-activity:
// Activity in board data). Times are RFC 3339 in local time with their
// offset; "" when unknown.
type Activity struct {
	Step   string `json:"step"`
	Agent  string `json:"agent"`
	Model  string `json:"model"`
	Effort string `json:"effort"`
	Round  int    `json:"round"`
	// Task is the task the run is on: the first unticked task of its scope
	// (or of the change) in the worktree's tasks.md. It is null when every
	// such task is ticked; TaskLabel then says scope.AllTicked.
	Task      *TaskRef `json:"task"`
	TaskLabel string   `json:"task_label"`
	// LastTick is the last task ticked in the current run, or null.
	LastTick     *Tick  `json:"last_tick"`
	StepStarted  string `json:"step_started"`
	RunStarted   string `json:"run_started"`
	LastActivity string `json:"last_activity"`
	// AgentState is "working" or "waiting for you" while the step runs,
	// else "".
	AgentState string `json:"agent_state"`
	// Backend is "terminal" or "headless".
	Backend string `json:"backend"`
	// Terminal is the agent's herdr-core terminal, for the terminal
	// backend while the step runs.
	Terminal *TerminalRef `json:"terminal,omitempty"`
}

// TaskRef is a task by number and text.
type TaskRef struct {
	Number string `json:"number"`
	Text   string `json:"text"`
}

// Tick is a task_ticked or task_unticked event.
type Tick struct {
	Number string `json:"number"`
	At     string `json:"at"`
	// Ticked is false for an untick.
	Ticked bool   `json:"ticked"`
	Step   string `json:"step"`
	Round  int    `json:"round"`
}

// TerminalRef is where an agent's terminal is: Watch focuses Address on
// Machine.
type TerminalRef struct {
	Address string `json:"address"`
	Name    string `json:"name"`
	Machine string `json:"machine"`
}

// lookTerminal asks herdr-core for an agent's terminal; tests replace it.
var lookTerminal = func(ctx context.Context, name string) (*agent.HCTerminal, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return agent.HC{}.Get(ctx, name)
}

// hasActivity reports whether a card in state s shows activity.
func hasActivity(s runs.State) bool {
	return s == runs.Running || s == runs.Queued || s == runs.Stopped
}

// readActivity builds a card's activity from its run r (as people see it),
// the change folder in its worktree, its state directory and history.
func readActivity(ctx context.Context, cfg *config.Config, r *runs.Run, folder string, st statedir.Dir, events []history.Event, machine string) *Activity {
	a := &Activity{Step: r.Step, Round: r.Round, Backend: r.AgentBackend, TaskLabel: scope.AllTicked}
	if step, ok := cfg.Step(r.Step); ok {
		a.Agent, a.Model, a.Effort = step.Agent, step.Model, step.Effort
		if ag, ok := cfg.Agents[step.Agent]; ok {
			if a.Model == "" {
				a.Model = ag.Model
			}
			if a.Effort == "" {
				a.Effort = ag.Effort
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(folder, "tasks.md")); err == nil {
		if t, ok := scope.Current(scope.Parse(string(data)), r.Scope); ok {
			a.Task = &TaskRef{Number: t.Number, Text: t.Text}
			a.TaskLabel = "on " + t.Number
		}
	}
	ticks, runStart := runTicks(events)
	for i := range ticks {
		if ticks[i].Ticked {
			a.LastTick = &ticks[i]
			break
		}
	}
	if runStart.IsZero() {
		runStart = r.Created
	}
	a.RunStarted = timestamp(runStart)
	var stepStart time.Time
	for _, e := range events {
		if e.Kind == history.StepStarted && e.Step == r.Step && e.Round == r.Round {
			stepStart = e.Time
		}
	}
	if stepStart.IsZero() && r.State == runs.Running {
		stepStart = r.Since
	}
	a.StepStarted = timestamp(stepStart)
	var last time.Time
	if st != "" && r.Step != "" {
		if fi, err := os.Stat(st.Log(r.Step)); err == nil {
			last = fi.ModTime()
		}
	}
	if r.State == runs.Running {
		a.AgentState = agent.AgentWorking
		if r.Terminal != nil {
			a.Backend = config.BackendTerminal
			a.Terminal = &TerminalRef{Address: r.Terminal.Address, Name: r.Terminal.Name, Machine: machine}
			if r.AgentState != "" {
				a.AgentState = r.AgentState
			}
			if t, err := lookTerminal(ctx, r.Terminal.Name); err == nil && t != nil {
				if at := t.LastActivity(); !at.IsZero() {
					last = at
				}
				if t.Live() && t.Waiting() {
					a.AgentState = agent.AgentWaiting
				}
				if a.Terminal.Address == "" {
					a.Terminal.Address = t.Address
				}
			}
		}
	}
	if a.Backend == "" {
		a.Backend = config.BackendHeadless
	}
	a.LastActivity = timestamp(last)
	return a
}

// runTicks returns the tick events of the current run, newest first, and
// when that run started: its last started, rerun or scope_started event (a
// resume continues the same run).
func runTicks(events []history.Event) ([]Tick, time.Time) {
	start := 0
	var at time.Time
	for i, e := range events {
		switch e.Kind {
		case history.Started, history.Rerun, history.ScopeStarted:
			start, at = i, e.Time
		}
	}
	ticks := []Tick{}
	for i := len(events) - 1; i >= start; i-- {
		e := events[i]
		if e.Kind == history.TaskTicked || e.Kind == history.TaskUnticked {
			ticks = append(ticks, Tick{Number: e.Task, At: timestamp(e.Time), Ticked: e.Kind == history.TaskTicked, Step: e.Step, Round: e.Round})
		}
	}
	return ticks, at
}
