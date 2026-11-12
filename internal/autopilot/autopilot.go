// Package autopilot is tuckerbox autopilot's loop: on each pass it looks at
// a repository's active changes in admission order and starts approved
// ones while max_parallel has room, group by group, resumes a crashed run
// once per crash streak, and reports runs that need a person.
//
// It keeps no state of its own: every decision comes from OpenSpec at the
// fetched base, run.json and history. Every start or resume it asks for is
// conditional (runs.Expect): the engine goes ahead only if the run is still
// as the pass saw it and the approval rule holds, under the change lock
// (formal/autopilot.tla).
package autopilot

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/deps"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/scope"
)

// By is the value of history's "by" on events autopilot asked for.
const By = "autopilot"

// Request is a start or resume a pass asks for.
type Request struct {
	Change string
	// Mode is runs.Start (a new run, or the next group) or runs.Resume (a
	// crashed run).
	Mode runs.Mode
	// Tasks is "next" in group mode, "" for the whole change.
	Tasks string
	// Worktree is the change's worktree, for a resume.
	Worktree string
	// StartRev is the commit the approval was judged at, for a new
	// worktree ("" when the change has one).
	StartRev string
	// Expect is the run as the pass saw it.
	Expect runs.Expect
}

// Args is the request as tuckerbox engine arguments, change last.
func (r Request) Args() []string {
	var a []string
	switch r.Mode {
	case runs.Resume:
		a = []string{"--mode", "resume", "--worktree", r.Worktree}
	default:
		a = []string{"--mode", "start"}
		if r.Tasks != "" {
			a = append(a, "--tasks", r.Tasks)
		}
		if r.StartRev != "" {
			a = append(a, "--start-rev", r.StartRev)
		}
	}
	if r.Expect.NoRun {
		a = append(a, "--expect-none")
	} else {
		a = append(a, "--expect-state", string(r.Expect.State), "--expect-since", r.Expect.Since.Format(time.RFC3339Nano))
	}
	return append(a, "--by", By, r.Change)
}

// Describe is how the terminal shows the request.
func (r Request) Describe() string {
	switch {
	case r.Mode == runs.Resume:
		return "resume " + r.Change + " (its run crashed)"
	case r.Tasks != "":
		return "start " + r.Change + " --tasks " + r.Tasks
	}
	return "start " + r.Change
}

// Pilot makes passes for one repository on this machine.
type Pilot struct {
	Repo   runs.Repo
	Config *config.Config
	// Out receives one line per thing done or needing a person.
	Out io.Writer
	// DryRun prints what a pass would start and starts nothing.
	DryRun bool
	// Approval judges change's approval where a run would start from (its
	// worktree, else the commit at base a new worktree would be made at),
	// as tuckerbox run does, and returns that commit ("" for a worktree).
	Approval func(ctx context.Context, change, base string) (st approval.State, startRev string, err error)
	// Start asks the engine for req in the background, as tuckerbox run
	// does. A *runs.ChangedError (or any error) means nothing was started.
	Start func(ctx context.Context, req Request) error
	// Order puts the active changes in admission order. Nil keeps them
	// sorted by id.
	Order func(ctx context.Context, ids []string) ([]string, error)
	// Warn receives problems that don't stop a pass (offline fetch).
	Warn func(string)

	// reported is what each change was last reported for, so a report is
	// printed once per state change.
	reported map[string]string
}

// Outcome is what a pass did, for tests and --dry-run.
type Outcome struct {
	// Requested lists the requests made (or that would be, with DryRun),
	// in order.
	Requested []Request
	// NeedsYou lists the changes that need a person, with why.
	NeedsYou map[string]string
}

func (p *Pilot) printf(format string, args ...any) {
	if p.Out != nil {
		fmt.Fprintf(p.Out, format+"\n", args...)
	}
}

// Pass makes one pass.
func (p *Pilot) Pass(ctx context.Context) (Outcome, error) {
	out := Outcome{NeedsYou: map[string]string{}}
	base, err := p.Repo.Base(ctx, p.Config.Worktree.Base, p.Warn)
	if err != nil {
		return out, err
	}
	rev, err := runs.RevOf(ctx, p.Repo, base)
	if err != nil {
		return out, err
	}
	ids, err := runs.ActiveAt(ctx, p.Repo, rev)
	if err != nil {
		return out, err
	}
	if p.Order != nil {
		if ids, err = p.Order(ctx, ids); err != nil {
			return out, err
		}
	}
	local, err := runs.LocalRuns(ctx, p.Repo)
	if err != nil {
		return out, err
	}
	busy := 0
	for _, l := range local {
		if l.Busy() {
			busy++
		}
	}
	room := p.Config.MaxParallel - busy
	lookup := runs.DependencyLookup(ctx, p.Repo, p.Config)
	active := map[string]bool{}
	for _, id := range ids {
		active[id] = true
	}

	for _, id := range ids {
		req, why, err := p.decide(ctx, id, base, rev, active, local, lookup, room)
		if err != nil {
			p.printf("%s: %v", id, err)
			continue
		}
		if why != "" {
			out.NeedsYou[id] = why
		}
		if req == nil {
			continue
		}
		if p.DryRun {
			p.printf("would %s", req.Describe())
		} else if err := p.Start(ctx, *req); err != nil {
			p.printf("%s: not started: %v", id, err)
			continue
		} else {
			p.printf("%s", req.Describe())
		}
		out.Requested = append(out.Requested, *req)
		if req.Mode == runs.Start {
			room--
		}
	}
	p.report(out.NeedsYou)
	return out, nil
}

// decide returns what to ask for one change, or why it needs a person.
func (p *Pilot) decide(ctx context.Context, id, base, rev string, active map[string]bool, local map[string]runs.LocalRun, lookup deps.Lookup, room int) (*Request, string, error) {
	l, hasRun := local[id]
	tasks := ""
	if p.Config.Autopilot.Scope != config.AutopilotChange {
		tasks = scope.Next
	}
	switch {
	case !hasRun:
		if room <= 0 {
			return nil, "", nil
		}
		st, startRev, err := p.Approval(ctx, id, base)
		if err != nil || st != approval.Approved {
			return nil, "", err
		}
		if met, err := p.depsMet(ctx, id, rev, active, lookup); err != nil || !met {
			return nil, "", err
		}
		return &Request{Change: id, Mode: runs.Start, Tasks: tasks, StartRev: startRev, Expect: runs.Expect{NoRun: true}}, "", nil
	case l.Active:
		// Running, queued, or held by a person's edit.
		return nil, "", nil
	case l.Crashed():
		return p.crashed(ctx, id, base, l)
	}
	r := l.Run
	switch r.State {
	case runs.Blocked:
		return nil, "blocked: " + r.Reason, nil
	case runs.Failed:
		return nil, "failed: " + r.Reason, nil
	case runs.Ready:
	default:
		// Stopped by a person: theirs to resume.
		return nil, "", nil
	}
	if r.Complete {
		// Finished here; it ships, or waits for a person in local mode.
		return nil, "", nil
	}
	if r.Scope == nil && tasks == "" {
		left := unticked(l)
		return nil, "ready but not complete (tasks not ticked: " + left + "); finish or re-run it by hand", nil
	}
	if room <= 0 {
		return nil, "", nil
	}
	st, _, err := p.Approval(ctx, id, base)
	if err != nil {
		return nil, "", err
	}
	if st != approval.Approved {
		return nil, fmt.Sprintf("needs approving again before its next group (approval is %s)", st), nil
	}
	return &Request{Change: id, Mode: runs.Start, Tasks: scope.Next, Expect: runs.Expect{State: r.State, Since: r.Since}}, "", nil
}

// crashed decides on a run whose process died: resume it once per crash
// streak, while its approval isn't withdrawn.
func (p *Pilot) crashed(ctx context.Context, id, base string, l runs.LocalRun) (*Request, string, error) {
	events, err := history.Read(history.File(l.State))
	if err != nil {
		return nil, "", err
	}
	if CrashStreak(events) > 0 {
		return nil, "its run crashed again after autopilot resumed it; resume it by hand", nil
	}
	st, _, err := p.Approval(ctx, id, base)
	if err != nil {
		return nil, "", err
	}
	if st == approval.None {
		return nil, "its run crashed and its approval was withdrawn", nil
	}
	return &Request{Change: id, Mode: runs.Resume, Worktree: l.Worktree,
		Expect: runs.Expect{State: l.Run.State, Since: l.Run.Since}}, "", nil
}

// CrashStreak counts autopilot's resumes since the run's last step result,
// or its last start, resume, rerun or new scope by a person. Rounds don't
// reset it (formal/autopilot.tla, CrashBudget).
func CrashStreak(events []history.Event) int {
	n := 0
	for _, e := range events {
		switch e.Kind {
		case history.StepFinished:
			n = 0
		case history.Started, history.Resumed, history.Rerun, history.ScopeStarted:
			if e.By == By {
				if e.Kind == history.Resumed {
					n++
				}
			} else {
				n = 0
			}
		}
	}
	return n
}

// depsMet judges change id's dependencies as the engine does, read-only,
// from its tuckerbox.toml at rev: each must be archived at rev, or an
// active change whose run on this machine counts as ready.
func (p *Pilot) depsMet(ctx context.Context, id, rev string, active map[string]bool, lookup deps.Lookup) (bool, error) {
	data, ok := runs.ShowAt(ctx, p.Repo, rev, "openspec/changes/"+id+"/"+deps.FileName)
	if !ok {
		return true, nil
	}
	var f deps.File
	if _, err := toml.Decode(data, &f); err != nil {
		return false, fmt.Errorf("%s: %v", deps.FileName, err)
	}
	if len(f.DependsOn) == 0 {
		return true, nil
	}
	archived, err := runs.ArchivedAt(ctx, p.Repo, rev)
	if err != nil {
		return false, err
	}
	for _, d := range f.DependsOn {
		d = strings.TrimSpace(d)
		switch {
		case d == "" || archived[d]:
		case !active[d]:
			return false, nil
		default:
			if state, _ := lookup(d); state != deps.Ready {
				return false, nil
			}
		}
	}
	return true, nil
}

func unticked(l runs.LocalRun) string {
	tasks, err := runs.ReadTasks(runs.TasksFile(l.Worktree, l.Run.Change))
	if err != nil {
		return "?"
	}
	left := scope.Unticked(tasks)
	if len(left) == 0 {
		return "none"
	}
	return strings.Join(left, ", ")
}

// report prints each change that newly needs a person, once per reason.
func (p *Pilot) report(now map[string]string) {
	if p.reported == nil {
		p.reported = map[string]string{}
	}
	for id, why := range now {
		if p.reported[id] != why {
			p.printf("%s needs you: %s", id, why)
		}
	}
	p.reported = now
}
