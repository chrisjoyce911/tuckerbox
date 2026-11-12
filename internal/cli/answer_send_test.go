package cli

import (
	"context"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
)

// handoffHC is herdr-core with the change's terminal name held, at each
// lookup, by the next terminal in terms; the terminal at an address in
// gone has ended (or been replaced) by the time anything is sent to it.
type handoffHC struct {
	terms []*agent.HCTerminal
	gets  int
	gone  map[string]bool
	sends []string
}

func (h *handoffHC) Get(_ context.Context, name string) (*agent.HCTerminal, error) {
	t := h.terms[min(h.gets, len(h.terms)-1)]
	h.gets++
	return t, nil
}

func (h *handoffHC) Send(_ context.Context, target, text string) error {
	h.sends = append(h.sends, target)
	if h.gone[target] {
		return &agent.HCError{Args: []string{"send", target}, Kind: "not_found", Message: "no terminal"}
	}
	return nil
}

func agentTerm(addr, step, round string) *agent.HCTerminal {
	return &agent.HCTerminal{Address: addr, Name: "tb-x", State: "live", Status: &agent.HCStatus{State: "waiting"},
		Labels: map[string]string{agent.LabelChange: "add-login", agent.LabelStep: step, agent.LabelRound: round}}
}

// The answer reaches only the terminal of the step and round the run
// named when answer read it, at its address: a terminal of the next step
// under the same name never gets it, whether it took the name before the
// lookup or between the lookup and the send.
func TestAnswerFollowsTheRunsTerminalOnly(t *testing.T) {
	captured := func() *runs.Run {
		return &runs.Run{Change: "add-login", State: runs.Running, Step: "review", Round: 1,
			AgentState: agent.AgentWaiting, Terminal: &runs.TerminalRef{Name: "tb-x", Address: "t1"}}
	}
	ctx := context.Background()

	// Its own terminal waits: the text goes to its address.
	hc := &handoffHC{terms: []*agent.HCTerminal{agentTerm("t1", "review", "1")}}
	if name, err := (App{}).sendWaiting(ctx, hc, captured(), "yes"); err != nil || name != "tb-x" {
		t.Fatalf("own terminal: %q %v", name, err)
	}
	if len(hc.sends) != 1 || hc.sends[0] != "t1" {
		t.Errorf("sent to %v, want the address t1", hc.sends)
	}

	// The workflow moved on after answer read run.json: the name holds the
	// build step's terminal, and nothing is typed.
	for _, next := range []*agent.HCTerminal{agentTerm("t2", "build", "2"), agentTerm("t1", "build", "2"), agentTerm("t2", "review", "1")} {
		hc = &handoffHC{terms: []*agent.HCTerminal{next}}
		if name, err := (App{}).sendWaiting(ctx, hc, captured(), "yes"); err != nil || name != "" || len(hc.sends) != 0 {
			t.Errorf("replaced by %s %s/%s: typed into %q (sends %v), err %v", next.Address,
				next.Labels[agent.LabelStep], next.Labels[agent.LabelRound], name, hc.sends, err)
		}
	}

	// Replaced between the lookup and the send: the send names the old
	// address, which herdr-core no longer has, so the new agent gets
	// nothing and nothing is recorded as typed.
	hc = &handoffHC{terms: []*agent.HCTerminal{agentTerm("t1", "review", "1")}, gone: map[string]bool{"t1": true}}
	if name, err := (App{}).sendWaiting(ctx, hc, captured(), "yes"); err != nil || name != "" {
		t.Errorf("replaced during the send: %q %v", name, err)
	}
	if len(hc.sends) != 1 || hc.sends[0] != "t1" {
		t.Errorf("sent to %v, want only the old address t1", hc.sends)
	}

	// A launch not yet confirmed (no address in run.json) is never typed into.
	rs := captured()
	rs.Terminal.Address = ""
	hc = &handoffHC{terms: []*agent.HCTerminal{agentTerm("t1", "review", "1")}}
	if name, _ := (App{}).sendWaiting(ctx, hc, rs, "yes"); name != "" || len(hc.sends) != 0 {
		t.Errorf("no address: typed into %q", name)
	}
}
