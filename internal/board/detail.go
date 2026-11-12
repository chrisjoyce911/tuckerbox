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
	"github.com/chrisjoyce911/tuckerbox/internal/scope"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

func readDetail(id, folder string, state statedir.Dir, run *runs.Run) (*Detail, error) {
	d := &Detail{Change: id, Specs: []Spec{}, Tasks: []Task{}, Rounds: []Round{}, History: []Event{}, Groups: []Group{}, Ticks: []Tick{}, Choices: noChoices()}
	var err error
	d.Proposal, err = readOptional(filepath.Join(folder, "proposal.md"))
	if err != nil {
		return d, err
	}
	specs, err := approval.Dir(folder).Specs()
	if err != nil {
		return d, err
	}
	for _, s := range specs {
		text, err := readOptional(filepath.Join(folder, filepath.FromSlash(s)))
		if err != nil {
			return d, err
		}
		d.Specs = append(d.Specs, Spec{Capability: strings.TrimPrefix(path.Dir(s), "specs/"), Text: text})
	}
	tasks, err := readOptional(filepath.Join(folder, "tasks.md"))
	if err != nil {
		return d, err
	}
	d.Tasks = parseTasks(tasks)
	d.Groups = groups(tasks)
	var events []history.Event
	if state != "" {
		events, err = history.Read(history.File(state))
		if err != nil {
			return d, err
		}
	}
	for _, e := range events {
		d.History = append(d.History, Event{At: timestamp(e.Time), Event: string(e.Kind), Step: e.Step, Round: e.Round, Agent: e.Agent, Machine: e.Machine, Result: string(e.Status), To: e.To, Summary: e.Summary, Findings: e.Findings, Reason: e.Reason, Scope: e.Scope, OldScope: e.OldScope, Commit: e.Commit, Branch: e.Branch, Link: e.Link, Number: e.Number, Method: e.Method, ShipRound: e.ShipRound, CI: e.CI, Jobs: e.Jobs, Threads: e.Threads, Task: e.Task})
	}
	d.Ticks, _ = runTicks(events)
	d.Rounds = rounds(run, events)
	roundScopes(d.Rounds, run, events)
	return d, nil
}

// groups counts each task group's tasks, leaving out (ship) tasks.
func groups(tasksMD string) []Group {
	out := []Group{}
	for _, g := range scope.Groups(tasksMD) {
		bg := Group{Number: g.Number, Title: g.Title}
		for _, t := range g.Tasks {
			if t.Ship {
				continue
			}
			bg.Total++
			if t.Checked {
				bg.Ticked++
			}
		}
		out = append(out, bg)
	}
	return out
}

// roundScopes sets each round's scope: the scope of the started, resumed,
// rerun or scope_started event before the round's first step. The run's
// current round has the run's own scope.
func roundScopes(rounds []Round, run *runs.Run, events []history.Event) {
	at := map[int]string{}
	cur := ""
	for _, e := range events {
		switch e.Kind {
		case history.Started, history.Resumed, history.Rerun, history.ScopeStarted:
			cur = e.Scope
		case history.StepStarted:
			if _, ok := at[e.Round]; !ok && e.Round > 0 {
				at[e.Round] = cur
			}
		}
	}
	if run != nil && run.Round > 0 {
		if _, ok := at[run.Round]; !ok {
			at[run.Round] = run.Scope.SpecOrEmpty()
		}
	}
	for i := range rounds {
		rounds[i].Scope = at[rounds[i].Round]
	}
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
		switch e.Kind {
		case history.StepStarted, history.StepFinished, history.Rejected, history.Blocked, history.Failed, history.Stopped:
		default:
			continue
		}
		// A pause before any step/result is not a round of work.
		if _, ok := byNumber[e.Round]; !ok && (e.Kind == history.Stopped || e.Kind == history.Blocked || e.Kind == history.Failed) {
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
		case history.Blocked, history.Failed, history.Stopped:
			r.Result, r.Step, r.Ended = string(e.Kind), e.Step, timestamp(e.Time)
			if r.Summary == "" {
				r.Summary = e.Reason
			}
		}
		byNumber[e.Round] = r
	}
	if run != nil && run.Round > 0 && (byNumber[run.Round].Round > 0 || (run.Step != "" && run.State == runs.Running)) {
		r := ensure(run.Round)
		switch run.State {
		case runs.Blocked, runs.Failed, runs.Stopped:
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
