package board

import (
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

func readDetail(id, folder string, state statedir.Dir, run *runs.Run) (*Detail, error) {
	d := &Detail{Change: id, Specs: []Spec{}, Tasks: []Task{}, Rounds: []Round{}, History: []Event{}}
	var err error
	d.Proposal, err = readOptional(filepath.Join(folder, "proposal.md"))
	if err != nil {
		return nil, err
	}
	specs, err := approval.Dir(folder).Specs()
	if err != nil {
		return nil, err
	}
	for _, s := range specs {
		text, err := readOptional(filepath.Join(folder, filepath.FromSlash(s)))
		if err != nil {
			return nil, err
		}
		d.Specs = append(d.Specs, Spec{Capability: strings.TrimPrefix(path.Dir(s), "specs/"), Text: text})
	}
	tasks, err := readOptional(filepath.Join(folder, "tasks.md"))
	if err != nil {
		return nil, err
	}
	d.Tasks = parseTasks(tasks)
	var events []history.Event
	if state != "" {
		events, err = history.Read(history.File(state))
		if err != nil {
			return nil, err
		}
	}
	for _, e := range events {
		d.History = append(d.History, Event{At: timestamp(e.Time), Event: string(e.Kind), Step: e.Step, Round: e.Round, Agent: e.Agent, Machine: e.Machine, Result: string(e.Status), To: e.To, Summary: e.Summary, Findings: e.Findings, Reason: e.Reason})
	}
	d.Rounds = rounds(run, events)
	return d, nil
}

// One round may finish several workflow steps; its last result wins. Retain
// findings when a lifecycle event (e.g. review-limit blocked) follows a result.
func rounds(run *runs.Run, events []history.Event) []Round {
	byNumber := map[int]Round{}
	ensure := func(n int) Round {
		r, ok := byNumber[n]
		if !ok {
			r = Round{Round: n, Findings: []agent.Finding{}}
		}
		return r
	}
	if run != nil {
		for _, e := range run.History {
			r := ensure(e.Round)
			r.Result, r.Step, r.Summary, r.Ended = string(e.Status), e.Step, e.Summary, timestamp(e.Ended)
			r.Findings = append([]agent.Finding{}, e.Findings...)
			byNumber[e.Round] = r
		}
	}
	// History supplies lifecycle results and also supports runs predating the
	// current snapshot's retained results. Apply events in recorded order.
	for _, e := range events {
		if e.Round <= 0 {
			continue
		}
		r := ensure(e.Round)
		switch e.Kind {
		case history.StepStarted:
			r.Result, r.Step, r.Summary, r.Ended = "", e.Step, "", ""
			r.Findings = []agent.Finding{}
		case history.StepFinished, history.Rejected:
			r.Result = string(e.Status)
			if e.Kind == history.Rejected {
				r.Result = "rejected"
			}
			r.Step, r.Summary, r.Ended = e.Step, e.Summary, timestamp(e.Time)
			r.Findings = append([]agent.Finding{}, e.Findings...)
		case history.Blocked, history.Failed:
			r.Result, r.Step, r.Ended = string(e.Kind), e.Step, timestamp(e.Time)
			if r.Summary == "" {
				r.Summary = e.Reason
			}
		}
		byNumber[e.Round] = r
	}
	if run != nil && run.Round > 0 {
		r := ensure(run.Round)
		switch run.State {
		case runs.Blocked, runs.Failed:
			r.Result, r.Step, r.Ended = string(run.State), run.Step, timestamp(run.Since)
			if r.Summary == "" {
				r.Summary = run.Reason
			}
		case runs.Running:
			r.Result, r.Step, r.Ended = "", run.Step, ""
		}
		byNumber[run.Round] = r
	}
	out := []Round{}
	for _, r := range byNumber {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Round < out[j].Round })
	return out
}
