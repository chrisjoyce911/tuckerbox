package runs

import (
	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
)

// event appends ev to the run's history, filling in the time, change,
// machine, and the step, round and agent from rs where ev leaves them
// unset. The caller holds the change lock. A failed write is logged and
// does not stop the run.
func (e *Engine) event(ws Workspace, rs *Run, ev history.Event) {
	ev.Time = e.now()
	ev.Change = rs.Change
	if ev.Step == "" {
		ev.Step = rs.Step
	}
	if ev.Round == 0 {
		ev.Round = rs.Round
	}
	if ev.Agent == "" && ev.Step != "" {
		for _, s := range e.Config.Steps {
			if s.ID == ev.Step {
				ev.Agent = s.Agent
			}
		}
	}
	if e.machine == "" {
		e.machine = history.Machine()
	}
	ev.Machine = e.machine
	if err := history.Append(history.File(ws.State), ev); err != nil {
		e.logf(rs.Change, "warning: history: %v", err)
	}
}

// startEvents records how a run began: started for a new run, else
// resumed or rerun. A run whose previous process died while queued or
// running (it left no final state) first gets the stopped event it never
// wrote. Called once the run is saved as queued, then queued itself.
func (e *Engine) startEvents(ws Workspace, rs *Run, mode Mode, prev Run, by string) {
	kind := history.Started
	sc := rs.Scope.SpecOrEmpty()
	if prev.Change != "" {
		if prev.State == Queued || prev.State == Running || prev.State == Shipping {
			e.event(ws, rs, history.Event{Kind: history.Stopped, Step: prev.Step, Round: prev.Round, Reason: Crashed})
		}
		kind = history.Resumed
		if mode == Rerun {
			kind = history.Rerun
		}
	} else if mode == Rerun {
		kind = history.Rerun
	}
	ev := history.Event{Kind: kind, Scope: sc}
	if prev.Change != "" && mode == Start && prev.Scope.SpecOrEmpty() != sc {
		ev = history.Event{Kind: history.ScopeStarted, Scope: sc, OldScope: prev.Scope.SpecOrEmpty()}
	}
	ev.By = by
	e.event(ws, rs, ev)
	e.event(ws, rs, history.Event{Kind: history.Queued})
}

// resultEvent records a step's accepted result.
func (e *Engine) resultEvent(ws Workspace, rs *Run, res *agent.Result, exitCode int) {
	e.event(ws, rs, history.Event{
		Kind: history.StepFinished, Status: res.Status, Summary: res.Summary,
		Findings: res.Findings, ExitCode: &exitCode,
	})
}

// finishKind is the history kind for a final state.
func finishKind(s State) history.Kind {
	switch s {
	case Blocked:
		return history.Blocked
	case Stopped:
		return history.Stopped
	case Ready:
		return history.Ready
	case Merged:
		return history.Kind("merged")
	default:
		return history.Failed
	}
}
