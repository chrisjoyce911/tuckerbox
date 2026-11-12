package runs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/scope"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// Mode is how a run starts.
type Mode int

const (
	// Start begins a change's run, or continues it from its current step
	// if it already has one that is not ready.
	Start Mode = iota
	// Resume continues a stopped or blocked run from its current step.
	Resume
	// Rerun starts again from a named step.
	Rerun
)

// Request is one run to make.
type Request struct {
	Change string
	Mode   Mode
	// Step is the step to start from, for Rerun.
	Step string
	// Worktree is the change's existing worktree, for Resume and Rerun.
	// When "", the worktree is found or made from the config patterns.
	Worktree string
	// StartRev, for Start with a Gate, is the commit the caller checked:
	// a new worktree is made at it, or refused if the change's branch is
	// no longer there.
	StartRev string
	// Scope, for Start, is the task scope to run (see package scope), or
	// "" for the whole change. It is judged again in the worktree under
	// the change lock. A scope other than the run's own starts a ready,
	// stopped, blocked or failed run again from the first step.
	Scope string
	// Expect, when set, makes the request conditional (tuckerbox
	// autopilot): once the change lock is held, the run must still be as
	// the caller saw it, else Run returns a *ChangedError and changes
	// nothing (formal/autopilot.tla, ActsOnSeenAttempt).
	Expect *Expect
	// By says who asked, for the history event that starts the run
	// ("autopilot"); "" is a person.
	By string
}

// Expect is the run state a conditional request was decided on: no run,
// or run.json's state and since as read (not the effective state).
type Expect struct {
	NoRun bool
	State State
	Since time.Time
}

// ChangedError means a conditional request found the run changed since
// its caller looked, or its approval rule no longer held; nothing was
// changed.
type ChangedError struct {
	Change string
	Why    string
}

func (e *ChangedError) Error() string {
	return fmt.Sprintf("change %s changed since it was looked at (%s); nothing done", e.Change, e.Why)
}

// mismatch reports why rs (nil: no run.json) is not as x expects, or "".
func (x *Expect) mismatch(rs *Run) string {
	switch {
	case x.NoRun && rs != nil:
		return "it has a run now"
	case x.NoRun:
		return ""
	case rs == nil:
		return "its run is gone"
	case rs.State != x.State:
		return fmt.Sprintf("it is %s, not %s", rs.State, x.State)
	case !rs.Since.Equal(x.Since):
		return "its run moved on"
	}
	return ""
}

// Engine runs one change's workflow in this process.
type Engine struct {
	// Forge overrides the authenticated session in local fake-forge tests.
	Forge        func(context.Context) (forge.Client, func(string) string, error)
	ShipSleep    func(context.Context, time.Duration) error
	workflowSlot *Slot
	Repo         Repo
	Config       *config.Config
	// StateHome is the user state directory holding the machine index; ""
	// leaves the index alone.
	StateHome string
	// Log receives progress lines; nil discards them.
	Log io.Writer
	// Poll is how often a queued run retries for a slot and a run checks
	// for a stop request (default 250ms).
	Poll time.Duration
	// Agent tunes agent.Run.
	Agent agent.Options
	// OnLocked is called once the change lock is held, run.json exists and
	// the run has joined the slot queue, or has started waiting on its
	// dependencies (it joins the queue once they are met). tuckerbox run starts changes one
	// by one and waits for this before starting the next, so queued runs
	// start in the order they were requested.
	OnLocked func(ws Workspace, r *Run)
	// Now returns the time; nil means time.Now.
	Now func() time.Time
	// Gate, when set, may refuse a Start request (the approval run gate).
	// It judges the commit a new worktree will be made at, before it is
	// made, and the worktree's files again once the change lock is held,
	// before the run state changes. Resume and Rerun are not gated.
	Gate func(ctx context.Context, change string, at StartPoint) error
	// ResumeGate, when set, judges a conditional (Expect) Resume under the
	// change lock: autopilot resumes a crashed run only while the change's
	// approval is not withdrawn (approved or stale, not none).
	ResumeGate func(ctx context.Context, change, worktree string) error

	mu      sync.Mutex
	saved   Run
	machine string
}

// ActiveError means the change already has an active run.
type ActiveError struct {
	Change, Worktree string
	Pid              int
}

func (e *ActiveError) Error() string {
	msg := fmt.Sprintf("change %s already has an active run", e.Change)
	if e.Pid > 0 {
		msg += fmt.Sprintf(" (pid %d)", e.Pid)
	}
	return msg + " in " + e.Worktree + "; see tuckerbox status, or tuckerbox stop " + e.Change
}

func (e *ActiveError) Unwrap() error { return ErrLocked }

// StateError means the run is in a state the request can't start from.
type StateError struct {
	Change string
	State  State
	Hint   string
}

func (e *StateError) Error() string {
	msg := fmt.Sprintf("change %s is %s", e.Change, e.State)
	if e.Hint != "" {
		msg += "; " + e.Hint
	}
	return msg
}

// StopFile is the stop request file in a state directory. tuckerbox stop
// creates it; the engine sees it, stops the agent, records stopped and
// removes it. Signals do the same on Unix; on Windows the file is the
// graceful way.
func StopFile(d statedir.Dir) string { return filepath.Join(string(d), "stop") }

// EngineLog is the engine's own log in a state directory.
func EngineLog(d statedir.Dir) string { return filepath.Join(string(d), "run.log") }

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Engine) logf(change, format string, args ...any) {
	if e.Log == nil {
		return
	}
	fmt.Fprintf(e.Log, "%s %s: %s\n", e.now().Format(time.RFC3339), change, fmt.Sprintf(format, args...))
}

func (e *Engine) poll() time.Duration {
	if e.Poll > 0 {
		return e.Poll
	}
	return 250 * time.Millisecond
}

// OpenWorkspace returns the existing worktree at path.
func OpenWorkspace(ctx context.Context, path string) (Workspace, error) {
	st, err := statedir.Resolve(ctx, path)
	if err != nil {
		return Workspace{}, err
	}
	ws := Workspace{Path: path, State: st}
	if b, err := git(ctx, path, "symbolic-ref", "--short", "HEAD"); err == nil {
		ws.Branch = b
	}
	return ws, nil
}

// Run takes the change through the workflow and returns its final run
// state. It returns an error, and changes nothing, when the change already
// has an active run (*ActiveError) or the request does not fit the run's
// state (*StateError). A run that stops, blocks or fails is not an error:
// see the returned Run's State and Reason.
//
// Cancelling ctx stops the run: the agent is stopped and the run recorded
// as stopped.
func (e *Engine) Run(ctx context.Context, req Request) (*Run, error) {
	var ws Workspace
	var err error
	if req.Worktree != "" {
		ws, err = OpenWorkspace(ctx, req.Worktree)
	} else {
		ws, err = e.ensureWorktree(ctx, req)
	}
	if err != nil {
		return nil, err
	}
	if ws.Created {
		e.logf(req.Change, "created worktree %s on branch %s from %s", ws.Path, ws.Branch, ws.Base)
	}

	// Pick: the change lock, taken atomically.
	lock, err := TryChangeLock(ws.State.RunLock())
	if errors.Is(err, ErrLocked) {
		return nil, LockedError(req.Change, ws.Path, ws.State)
	}
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	// We hold the lock, so an edit marker is from an edit that died.
	RemoveEditMarker(ws.State)
	if req.Expect != nil {
		cur, err := ReadRun(ws.State.RunFile())
		switch {
		case errors.Is(err, ErrNoRun):
			cur = nil
		case err != nil:
			return nil, err
		}
		if why := req.Expect.mismatch(cur); why != "" {
			return nil, &ChangedError{Change: req.Change, Why: why}
		}
		if req.Mode == Resume && e.ResumeGate != nil {
			if err := e.ResumeGate(ctx, req.Change, ws.Path); err != nil {
				return nil, &ChangedError{Change: req.Change, Why: err.Error()}
			}
		}
	}
	if e.gated(req) {
		// Under the change lock, which approve and unapprove also take:
		// what is judged here is what the run starts with.
		if err := e.Gate(ctx, req.Change, StartPoint{Worktree: ws.Path}); err != nil {
			return nil, err
		}
	}
	if err := ws.State.Ensure(); err != nil {
		return nil, err
	}
	_ = os.Remove(StopFile(ws.State))

	rs, startIdx, err := e.prepare(req, ws)
	if err != nil {
		return nil, err
	}
	prev := e.saved
	pidPath := ws.State.PidFile()
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		return nil, err
	}
	defer os.Remove(pidPath)

	rs.Pid = os.Getpid()
	rs.AgentPid = 0
	rs.State = Queued
	// Leaving ready clears the completeness record in this same write.
	rs.Complete = false
	rs.Reason, rs.Tail = "", ""
	if err := e.save(ws, rs); err != nil {
		return nil, err
	}
	e.startEvents(ws, rs, req.Mode, prev, req.By)
	if e.StateHome != "" {
		if err := Remember(e.StateHome, IndexEntry{Repo: e.Repo.Main, Change: req.Change, Worktree: ws.Path, State: string(ws.State)}); err != nil {
			e.logf(req.Change, "warning: machine index: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopReq := e.watchStop(ctx, cancel, ws)
	notified := false
	notify := func() {
		if !notified && e.OnLocked != nil {
			e.OnLocked(ws, rs)
		}
		notified = true
	}

	if rs.ShippingStarted {
		notify()
		return e.forgeShip(ctx, ws, rs, true)
	}

	dw, fail := e.newDepWait(ws, rs)
	if fail != "" {
		return e.finish(ws, rs, Failed, fail)
	}
	var slot *Slot
	for slot == nil {
		// Wait for dependencies before joining the slot queue, so a
		// waiting run holds neither a slot nor a place in the queue.
		if fail, err := dw.wait(ctx, notify); ctx.Err() != nil {
			return e.finish(ws, rs, Stopped, stopReason(stopReq, "while waiting on dependencies"))
		} else if err != nil {
			return rs, err
		} else if fail != "" {
			return e.finish(ws, rs, Failed, fail)
		}

		// TakeSlot: wait in queue order for one of max_parallel slots.
		q, err := JoinQueue(e.Repo.Common, req.Change, ws.State.RunLock(), e.now().UnixNano())
		if err != nil {
			return e.finish(ws, rs, Failed, "joining the queue: "+err.Error())
		}
		notify()
		s, err := WaitSlot(ctx, e.Repo.Common, e.Config.MaxParallel, q, e.poll(), func() {
			e.logf(req.Change, "queued: all %d slots are taken", e.Config.MaxParallel)
		})
		q.Leave()
		if err != nil {
			if ctx.Err() != nil {
				return e.finish(ws, rs, Stopped, stopReason(stopReq, "while queued"))
			}
			return e.finish(ws, rs, Failed, "waiting for a slot: "+err.Error())
		}
		// A dependency may have left ready while this run waited for the
		// slot (it was rerun): look again, and if so give the slot back
		// and wait for the dependencies once more.
		met, fail, err := dw.check(ctx)
		switch {
		case err != nil:
			s.Release()
			return rs, err
		case fail != "":
			s.Release()
			return e.finish(ws, rs, Failed, fail)
		case !met:
			s.Release()
			e.logf(req.Change, "a dependency is no longer ready; gave the slot back")
			continue
		}
		slot = s
	}
	// Release: the slot is freed after the final state is written (the
	// deferred lock release runs after this one).
	defer slot.Release()
	e.workflowSlot = slot
	e.logf(req.Change, "running in slot %d", slot.N)

	return e.steps(ctx, ws, rs, startIdx, stopReq)
}

func (e *Engine) gated(req Request) bool { return e.Gate != nil && req.Mode == Start }

// ensureWorktree finds or makes req's worktree. With a Gate, a new one is
// made at a commit the gate has judged.
func (e *Engine) ensureWorktree(ctx context.Context, req Request) (Workspace, error) {
	warn := func(msg string) { e.logf(req.Change, "%s", msg) }
	if !e.gated(req) {
		return e.Repo.EnsureWorktree(ctx, e.Config.Worktree, req.Change, warn)
	}
	sp, err := e.Repo.ResolveStart(ctx, e.Config.Worktree, req.Change, "", warn)
	if err != nil {
		return Workspace{}, err
	}
	if sp.Worktree == "" {
		if req.StartRev != "" {
			if sp.Branch && sp.Rev != req.StartRev {
				return Workspace{}, fmt.Errorf("branch %s moved since change %s was checked; run it again", sp.Ref, req.Change)
			}
			sp.Rev = req.StartRev
		}
		if err := e.Gate(ctx, req.Change, sp); err != nil {
			return Workspace{}, err
		}
	}
	return e.Repo.EnsureWorktreeFrom(ctx, e.Config.Worktree, req.Change, &sp, warn)
}

// prepare reads or makes the run state for req and returns the index of
// the step to start from. The caller holds the change lock.
func (e *Engine) prepare(req Request, ws Workspace) (*Run, int, error) {
	steps := e.Config.Steps
	if len(steps) == 0 {
		return nil, 0, errors.New("the workflow has no steps")
	}
	rs, err := ReadRun(ws.State.RunFile())
	switch {
	case errors.Is(err, ErrNoRun):
		if req.Mode == Resume {
			return nil, 0, &StateError{Change: req.Change, State: "not started", Hint: "use tuckerbox run " + req.Change}
		}
		sc, _, err := e.startScope(req, ws, nil)
		if err != nil {
			return nil, 0, err
		}
		now := e.now()
		rs = &Run{
			Change: req.Change, Repo: e.Repo.Root, Worktree: ws.Path, Branch: ws.Branch, Base: ws.Base,
			Step: steps[0].ID, Round: 1, Created: now, Since: now, Scope: sc,
		}
		if req.Mode == Rerun {
			rs.Step = req.Step
		}
		e.saved = Run{}
		idx, err := e.stepIndex(rs.Step)
		return rs, idx, err
	case err != nil:
		return nil, 0, err
	}
	e.saved = *rs
	// We hold the lock, so a queued or running state is from a process
	// that died: it counts as stopped.
	cur := rs.Effective(false)
	switch req.Mode {
	case Start:
		sc, same, err := e.startScope(req, ws, rs)
		if err != nil {
			return nil, 0, err
		}
		if !same {
			// Another scope: the run starts again from the first step,
			// in the same worktree, with its own max_rounds.
			rs.Scope = sc
			rs.Step = steps[0].ID
			rs.Rejections = nil
		} else if cur.State == Ready {
			return nil, 0, &StateError{Change: req.Change, State: Ready, Hint: "its work is on branch " + rs.Branch + "; use tuckerbox rerun " + req.Change + " <step> to run a step again, or tuckerbox run " + req.Change + " --tasks <scope> to run other tasks"}
		}
	case Resume:
		switch cur.State {
		case Stopped, Blocked:
		case Ready:
			if e.Config.Ship != config.ShipMR || rs.MR == nil || rs.MR.State != "open" {
				return nil, 0, &StateError{Change: req.Change, State: Ready, Hint: "only a stopped or blocked run can be resumed"}
			}
		case Failed:
			return nil, 0, &StateError{Change: req.Change, State: Failed, Hint: "use tuckerbox rerun " + req.Change + " " + rs.Step + " to run the step again"}
		default:
			return nil, 0, &StateError{Change: req.Change, State: cur.State, Hint: "only a stopped or blocked run can be resumed"}
		}
		rs.Rejections = nil
	case Rerun:
		rs.Step = req.Step
		rs.Rejections = nil
	}
	if rs.ShippingStarted && req.Mode != Rerun {
		rs.Round++
		rs.Worktree = ws.Path
		rs.Repo = e.Repo.Root
		rs.Step = ""
		return rs, len(steps), nil
	}
	if rs.Step == "" {
		rs.Step = steps[0].ID
	}
	idx, err := e.stepIndex(rs.Step)
	if err != nil {
		return nil, 0, err
	}
	// A new attempt at a step is a new round, so nothing an earlier
	// attempt's agent writes can be accepted.
	rs.Round++
	rs.Worktree, rs.Repo = ws.Path, e.Repo.Root
	if ws.Branch != "" {
		rs.Branch = ws.Branch
	}
	return rs, idx, nil
}

func (e *Engine) stepIndex(id string) (int, error) {
	var ids []string
	for i, s := range e.Config.Steps {
		if s.ID == id {
			return i, nil
		}
		ids = append(ids, s.ID)
	}
	return 0, fmt.Errorf("step %q is not in the workflow (%s)", id, strings.Join(ids, ", "))
}

// steps runs the workflow from step index i.
func (e *Engine) steps(ctx context.Context, ws Workspace, rs *Run, i int, stopReq *stopRequest) (*Run, error) {
	steps := e.Config.Steps
	templates := map[string]agent.Template{}
	for name, a := range e.Config.Agents {
		templates[name] = agent.Template{Name: name, Command: a.Command}
	}
	for i < len(steps) {
		st := steps[i]
		if ctx.Err() != nil {
			return e.finish(ws, rs, Stopped, stopReason(stopReq, "before step "+st.ID))
		}
		rs.State, rs.Step, rs.Reason, rs.Tail = Running, st.ID, "", ""
		if err := e.save(ws, rs); err != nil {
			return rs, err
		}
		e.event(ws, rs, history.Event{Kind: history.StepStarted})
		tmpl, err := agent.Resolve(st.Agent, templates)
		if err != nil {
			return e.finish(ws, rs, Failed, err.Error())
		}
		model, effort := st.Model, st.Effort
		if a, ok := e.Config.Agents[st.Agent]; ok {
			model = firstNonEmpty(model, a.Model)
			effort = firstNonEmpty(effort, a.Effort)
		}
		e.logf(rs.Change, "step %s round %d: starting agent %s", st.ID, rs.Round, st.Agent)
		opts := e.Agent
		opts.OnStart = func(pid int) {
			rs.AgentPid = pid
			_ = e.save(ws, rs)
		}
		out, err := agent.Run(ctx, agent.Step{
			Change: rs.Change, Step: st.ID, Role: st.Role, Round: rs.Round,
			Agent: tmpl, Model: model, Effort: effort,
			Permissions: e.Config.PermissionArgs(st),
			Worktree:    ws.Path, State: ws.State, Base: rs.Base,
			Earlier: rs.Earlier(), Scope: rs.Scope,
		}, opts)
		rs.AgentPid = 0
		switch {
		case err != nil:
			return e.finish(ws, rs, Failed, err.Error())
		case out.Stopped:
			return e.finish(ws, rs, Stopped, stopReason(stopReq, "during step "+st.ID))
		case out.Result == nil:
			rs.Tail = out.Tail
			return e.finish(ws, rs, Failed, fmt.Sprintf("step %s: %s", st.ID, out.FailureReason()))
		}
		res := out.Result
		rs.History = append(rs.History, Entry{
			Step: st.ID, Round: rs.Round, Status: res.Status, Summary: res.Summary,
			Findings: res.Findings, ExitCode: out.ExitCode, Ended: e.now(),
		})
		e.logf(rs.Change, "step %s round %d: %s: %s", st.ID, rs.Round, res.Status, oneLine(res.Summary))
		e.resultEvent(ws, rs, res, out.ExitCode)
		switch res.Status {
		case agent.Done:
			if i+1 < len(steps) {
				e.event(ws, rs, history.Event{Kind: history.Handoff, To: steps[i+1].ID, Summary: res.Summary})
			}
			i++
		case agent.Rejected:
			if rs.Rejections == nil {
				rs.Rejections = map[string]int{}
			}
			rs.Rejections[st.ID]++
			n := rs.Rejections[st.ID]
			rej := history.Event{Kind: history.Rejected, Summary: res.Summary, Findings: res.Findings}
			if st.OnReject != "" && n < st.MaxRounds {
				rej.To = st.OnReject
			}
			e.event(ws, rs, rej)
			if st.OnReject == "" {
				return e.finish(ws, rs, Blocked, fmt.Sprintf("step %s rejected the change and has no on_reject: %s", st.ID, oneLine(res.Summary)))
			}
			if n >= st.MaxRounds {
				return e.finish(ws, rs, Blocked, fmt.Sprintf("review limit reached: step %s rejected %d times (max_rounds = %d)", st.ID, n, st.MaxRounds))
			}
			j, err := e.stepIndex(st.OnReject)
			if err != nil {
				return e.finish(ws, rs, Failed, err.Error())
			}
			// Accept with a rejection: the next round.
			rs.Round++
			i = j
		case agent.Blocked:
			return e.finish(ws, rs, Blocked, fmt.Sprintf("step %s is blocked: %s", st.ID, oneLine(res.Summary)))
		default: // agent.Failed
			return e.finish(ws, rs, Failed, fmt.Sprintf("step %s failed: %s", st.ID, oneLine(res.Summary)))
		}
	}
	return e.ship(ctx, ws, rs)
}

// ship finishes a run whose last step is done. A scoped run is ready only
// when its scope's tasks are ticked in the worktree. Whether the change is
// complete decides, with the ship mode, whether the run ships (see
// shipDecision). In local mode the work is committed on the change's
// branch and nothing is pushed.
func (e *Engine) ship(ctx context.Context, ws Workspace, rs *Run) (*Run, error) {
	// The worktree's tasks.md, read while this run holds the change lock:
	// tuckerbox tasks and edit write it only under that lock.
	tasks, err := readTasks(TasksFile(ws.Path, rs.Change))
	if err != nil {
		return e.finish(ws, rs, Failed, "reading tasks.md: "+err.Error())
	}
	if rs.Scope != nil {
		if reason := scopeReason(scope.Check(rs.Scope, tasks)); reason != "" {
			return e.finish(ws, rs, Blocked, reason)
		}
	}
	left := scope.Unticked(tasks)
	switch shipDecision(e.Config.Ship, rs.Scope != nil, len(left) == 0) {
	case outcomeBlocked:
		return e.finish(ws, rs, Blocked, "tasks not ticked: "+strings.Join(left, ", "))
	case outcomeShip:
		return e.forgeShip(ctx, ws, rs, false)
	}
	msg := fmt.Sprintf("%s: work from tuckerbox run\n\nCommitted by Tuckerbox (ship = \"local\") after the last step, %s, reported done.\n",
		rs.Change, rs.Step)
	made, err := CommitAll(context.Background(), ws.Path, msg)
	if err != nil {
		return e.finish(ws, rs, Failed, "final commit: "+err.Error())
	}
	if made {
		e.logf(rs.Change, "committed the remaining work on branch %s", rs.Branch)
	}
	rs.Step = ""
	// Recorded in the same run.json write that makes the run ready.
	rs.Complete = len(left) == 0
	return e.finish(ws, rs, Ready, "")
}

// finish records the run's final state for this process. The caller's
// deferred releases then free the slot and the change lock.
func (e *Engine) finish(ws Workspace, rs *Run, s State, reason string) (*Run, error) {
	rs.State, rs.Reason = s, reason
	rs.Pid, rs.AgentPid = 0, 0
	if s != Failed {
		rs.Tail = ""
	}
	if reason != "" {
		e.logf(rs.Change, "%s: %s", s, reason)
	} else {
		e.logf(rs.Change, "%s", s)
	}
	if err := e.save(ws, rs); err != nil {
		return rs, err
	}
	ev := history.Event{Kind: finishKind(s), Reason: reason}
	if s == Ready || s == Blocked {
		ev.Scope = rs.Scope.SpecOrEmpty()
	}
	if s != Merged {
		e.event(ws, rs, ev)
	}
	_ = os.Remove(StopFile(ws.State))
	return rs, nil
}

// save writes run.json, moving Since when the state, step or round changed.
func (e *Engine) save(ws Workspace, rs *Run) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if rs.State != e.saved.State || rs.Step != e.saved.Step || rs.Round != e.saved.Round || rs.Since.IsZero() {
		rs.Since = e.now()
	}
	if err := WriteRun(ws.State.RunFile(), rs); err != nil {
		return err
	}
	e.saved = *rs
	return nil
}

// stopRequest records whether the run was asked to stop by tuckerbox stop
// (its stop file), as opposed to a signal alone.
type stopRequest struct {
	mu   sync.Mutex
	seen bool
	path string
}

func (s *stopRequest) set() {
	s.mu.Lock()
	s.seen = true
	s.mu.Unlock()
}

// get reports a stop request. tuckerbox stop writes the file before it
// signals, so a signal that arrives first still counts as its request.
func (s *stopRequest) get() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.seen {
		if _, err := os.Stat(s.path); err == nil {
			s.seen = true
		}
	}
	return s.seen
}

// watchStop cancels the run when the stop file appears.
func (e *Engine) watchStop(ctx context.Context, cancel func(), ws Workspace) *stopRequest {
	p := StopFile(ws.State)
	req := &stopRequest{path: p}
	go func() {
		t := time.NewTicker(e.poll())
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, err := os.Stat(p); err == nil {
					req.set()
					cancel()
					return
				}
			}
		}
	}()
	return req
}

func stopReason(r *stopRequest, when string) string {
	if r != nil && r.get() {
		return "stopped by tuckerbox stop " + when
	}
	return "stopped " + when
}

func readPid(p string) (int, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// ReadPid returns the pid in a state directory's pid file, or 0.
func ReadPid(d statedir.Dir) int {
	pid, _ := readPid(d.PidFile())
	return pid
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// startScope judges req's scope in the worktree's tasks.md for a Start.
// cur is the run's current state, or nil for a new run; same reports that
// it already has that scope.
func (e *Engine) startScope(req Request, ws Workspace, cur *Run) (*scope.Scope, bool, error) {
	if req.Mode != Start {
		return nil, true, nil
	}
	var text string
	if req.Scope != "" {
		data, err := os.ReadFile(TasksFile(ws.Path, req.Change))
		if err != nil && !os.IsNotExist(err) {
			return nil, false, err
		}
		text = string(data)
	}
	sc, same, err := ScopeFor(req.Scope, text, cur)
	if err != nil {
		return nil, false, fmt.Errorf("change %s: %w", req.Change, err)
	}
	return sc, same, nil
}
