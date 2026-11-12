package runs

import (
	"context"
	"fmt"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
)

func (e *Engine) shippingFix(ctx context.Context, ws Workspace, rs *Run, stepID string, findings []agent.Finding) (*agent.Result, error) {
	step, ok := e.Config.Step(stepID)
	if !ok {
		return nil, fmt.Errorf("unknown shipping fix step %q", stepID)
	}
	rs.State, rs.Step, rs.Reason = Shipping, stepID, "queued for fix step"
	if err := e.save(ws, rs); err != nil {
		return nil, err
	}
	queue, err := JoinQueue(e.Repo.Common, e.Repo.Checkout(), rs.Change, ws.State.RunLock(), e.now().UnixNano())
	if err != nil {
		return nil, err
	}
	defer queue.Leave()
	slot, err := WaitSlot(ctx, e.Repo.Common, e.Config.MaxParallel, queue, e.poll(), nil)
	if err != nil {
		return nil, err
	}
	queue.Leave()
	defer slot.Release()
	rs.Round++
	rs.Reason = ""
	if err := e.save(ws, rs); err != nil {
		return nil, err
	}
	e.event(ws, rs, history.Event{Kind: history.StepStarted, Agent: step.Agent})
	template, err := agent.Resolve(step.Agent, e.templates())
	if err != nil {
		return nil, err
	}
	model, effort := step.Model, step.Effort
	if a, ok := e.Config.Agents[step.Agent]; ok {
		model = firstNonEmpty(model, a.Model)
		effort = firstNonEmpty(effort, a.Effort)
	}
	opts := e.Agent
	opts.OnStart = func(pid int) { rs.AgentPid = pid; _ = e.save(ws, rs) }
	// A shipping fix runs outside the run's workflow slot record, so it
	// runs headless (stepBackend) even under the terminal backend.
	out, err := e.stepBackend(template, agent.TermInfo{Slot: -1}).Run(ctx, agent.Step{Change: rs.Change, Step: stepID, Role: step.Role, Round: rs.Round, Agent: template, Model: model, Effort: effort, Permissions: e.Config.PermissionArgs(step), Worktree: ws.Path, State: ws.State, Base: rs.Base, ShippingFeedback: true, Earlier: []agent.Result{{Change: rs.Change, Step: "shipping", Round: rs.Round - 1, Status: agent.Rejected, Summary: "Forge findings to fix", Findings: findings}}}, opts)
	rs.AgentPid = 0
	if err != nil {
		return nil, err
	}
	if out.Stopped {
		return nil, ctx.Err()
	}
	if out.Result == nil {
		rs.Tail = out.Tail
		return nil, fmt.Errorf("fix step %s: %s", stepID, out.FailureReason())
	}
	result := out.Result
	rs.History = append(rs.History, Entry{Step: stepID, Round: rs.Round, Status: result.Status, Summary: result.Summary, Findings: result.Findings, ExitCode: out.ExitCode, Ended: e.now()})
	e.resultEvent(ws, rs, result, out.ExitCode)
	rs.Step = ""
	if err := e.save(ws, rs); err != nil {
		return nil, err
	}
	return result, nil
}
