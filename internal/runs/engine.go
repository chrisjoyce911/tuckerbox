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
	// Agent tunes the backend's Run.
	Agent agent.Options
	// Backend runs each step's agent; nil means agent.Headless.
	Backend agent.Backend
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
	// term is the run's terminal name, labels and slot, for the terminal
	// backend.
	term agent.TermInfo
	// adopting is the live agent this run adopted, from when its slot is
	// taken back until its step's backend runs it.
	adopting *adoption
	// afterSlot, when set, is called once the run holds its slot (tests).
	afterSlot func()
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

// templates are the agents config defines, as templates.
func (e *Engine) templates() map[string]agent.Template {
	templates := map[string]agent.Template{}
	for name, a := range e.Config.Agents {
		templates[name] = agent.Template{Name: name, Command: a.Command, Interactive: a.Interactive}
	}
	return templates
}

// stepBackend is the backend a step's agent runs in: the engine's, except
// that an agent without an interactive command, or a step outside a slot,
// runs headless under the terminal backend (agent-terminals: Agents that
// run in a terminal).
func (e *Engine) stepBackend(tmpl agent.Template, term agent.TermInfo) agent.Backend {
	b := e.backend()
	if _, ok := b.(agent.Adopter); ok && (!tmpl.HasInteractive() || term.Name == "" || term.Slot < 0) {
		return agent.Headless{}
	}
	return b
}

// agentOptions are the backend options for a step of rs: the engine's,
// with callbacks that record the agent's pid, terminal and state.
func (e *Engine) agentOptions(ws Workspace, rs *Run) agent.Options {
	opts := e.Agent
	opts.OnStart = func(pid int) {
		rs.AgentPid = pid
		_ = e.save(ws, rs)
	}
	opts.OnTerminal = func(name, address string) {
		rs.Terminal = &TerminalRef{Name: name, Address: address}
		_ = e.save(ws, rs)
	}
	opts.OnAgentState = func(state string) {
		rs.AgentState = state
		_ = e.save(ws, rs)
	}
	return opts
}

// adoption is a live agent a resumed run adopts.
type adoption struct {
	Name    string
	Backend agent.Adopter
}

// adoptable returns the live agent a Resume request adopts, or nil: the
// run's process died (we hold the lock) while it was running a step in a
// terminal, or while an earlier resume was taking that agent's slot back
// (queued), and a live terminal of that name has the run's change, step
// and round, and the run recorded its slot.
func (e *Engine) adoptable(ctx context.Context, req Request, ws Workspace) *adoption {
	b, ok := e.backend().(agent.Adopter)
	if !ok || req.Mode != Resume {
		return nil
	}
	rs, err := ReadRun(ws.State.RunFile())
	if err != nil || (rs.State != Running && rs.State != Queued) || rs.Terminal == nil || rs.Slot == nil {
		return nil
	}
	t, err := b.LiveAgent(ctx, rs.Terminal.Name)
	if err != nil {
		e.logf(req.Change, "warning: looking for the agent's terminal %s: %v", rs.Terminal.Name, err)
		return nil
	}
	if !agent.MatchesStep(t, rs.Change, rs.Step, rs.Round) {
		return nil
	}
	return &adoption{Name: rs.Terminal.Name, Backend: b}
}

// endLeftover ends a live agent terminal of the change that this run does
// not adopt, and waits until it is gone, before the run is admitted. The
// caller holds the change lock, so such a terminal's run process died: its
// agent still holds its slot's agent lock, and with max_parallel = 1 the
// run would otherwise wait for that slot forever (formal/terminals.tla,
// Launch: adopt or end the live agent first).
func (e *Engine) endLeftover(ctx context.Context, change, name string) error {
	b, ok := e.backend().(agent.Adopter)
	if !ok {
		return nil
	}
	t, err := b.LiveAgent(ctx, name)
	if err != nil {
		return fmt.Errorf("change %s: looking for its agent's terminal %s: %w", change, name, err)
	}
	if t == nil {
		return nil
	}
	e.logf(change, "ending the agent left in terminal %s (step %s round %s) before this run starts",
		name, t.Labels[agent.LabelStep], t.Labels[agent.LabelRound])
	if err := b.EndAgent(ctx, name); err != nil {
		return fmt.Errorf("change %s: %w", change, &agent.EndError{Name: name, Err: err})
	}
	return nil
}

// backend is the backend that runs step agents: e.Backend, or headless.
func (e *Engine) backend() agent.Backend {
	if e.Backend != nil {
		return e.Backend
	}
	return agent.Headless{}
}

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

	// Adopt: a resumed run whose process died while its agent works on in
	// a terminal of this step and round keeps that agent, its round and
	// its slot (formal/terminals.tla and liveslots.tla, Adopt).
	adopt := e.adoptable(ctx, req, ws)
	rs, startIdx, err := e.prepare(req, ws, adopt != nil)
	if err != nil {
		return nil, err
	}
	termName := agent.TerminalName(e.Repo.Identity(ctx), req.Change)
	if adopt == nil {
		if err := e.endLeftover(ctx, req.Change, termName); err != nil {
			return nil, err
		}
		// Nothing of an earlier process's agent is left to find.
		rs.Terminal, rs.Slot, rs.AgentState = nil, nil, ""
	}
	e.term = agent.TermInfo{
		Name:   termName,
		Space:  filepath.Base(e.Repo.Main),
		RunID:  strconv.FormatInt(rs.Created.UnixNano(), 10),
		Common: e.Repo.Common,
		Slot:   -1,
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

	var slot *Slot
	e.adopting = nil
	if adopt != nil {
		n := *rs.Slot
		e.logf(req.Change, "adopting the live agent in terminal %s (step %s round %d): taking its slot %d", adopt.Name, rs.Step, rs.Round, n)
		notify()
		s, err := TakeOwnSlot(ctx, e.Repo.Common, n, e.poll(), func() bool {
			t, err := adopt.Backend.LiveAgent(ctx, adopt.Name)
			return err != nil || (t != nil && agent.MatchesStep(t, rs.Change, rs.Step, rs.Round))
		})
		switch {
		case ctx.Err() != nil:
			// Stopped before the agent was adopted: it still works in its
			// terminal. End it, and know it is gone, before recording
			// stopped; one that would not end leaves the run as found.
			if s != nil {
				s.Release()
			}
			return e.stopAdopted(ws, rs, adopt, stopReason(stopReq, "while taking its slot back"))
		case err != nil:
			// The agent may still work in its terminal: keep its name and
			// slot so that stop and resume find it.
			return e.unwatched(ws, rs, &agent.EndError{Name: adopt.Name, Err: fmt.Errorf("taking slot %d back: %w", n, err)})
		case s == nil:
			// The agent ended before its slot was free: a new round, admitted
			// as any other run.
			e.logf(req.Change, "the agent in terminal %s ended before it could be adopted; starting a new round", adopt.Name)
			rs.Round++
			if err := e.save(ws, rs); err != nil {
				return rs, err
			}
		default:
			slot = s
			// Until its step's backend has it, the adopted agent is this
			// run's to end on a stop (see steps).
			e.adopting = adopt
		}
	}
	dw, fail := e.newDepWait(ws, rs)
	if fail != "" && slot == nil {
		return e.finish(ws, rs, Failed, fail)
	}
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

		// TakeSlot: wait in admission order for one of max_parallel slots.
		q, err := JoinQueue(e.Repo.Common, e.Repo.Checkout(), req.Change, ws.State.RunLock(), e.now().UnixNano())
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
	e.term.Slot = slot.N
	e.term.Owner = slot.Owner
	rs.Slot = &slot.N
	if err := e.save(ws, rs); err != nil {
		return rs, err
	}
	e.logf(req.Change, "running in slot %d", slot.N)
	if e.afterSlot != nil {
		e.afterSlot()
	}

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
func (e *Engine) prepare(req Request, ws Workspace, adopt bool) (*Run, int, error) {
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
	// attempt's agent writes can be accepted. An adopted agent is the
	// same attempt: its round stays.
	if !adopt {
		rs.Round++
	}
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
	templates := e.templates()
	for i < len(steps) {
		st := steps[i]
		if ctx.Err() != nil {
			if e.adopting != nil {
				// Stopped after the adopted agent's slot was taken back but
				// before its step watches it: it still works in its
				// terminal.
				return e.stopAdopted(ws, rs, e.adopting, stopReason(stopReq, "before step "+st.ID))
			}
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
		backend := e.stepBackend(tmpl, e.term)
		rs.AgentBackend = backend.Name()
		e.logf(rs.Change, "step %s round %d: starting agent %s (%s)", st.ID, rs.Round, st.Agent, backend.Name())
		opts := e.agentOptions(ws, rs)
		out, err := backend.Run(ctx, agent.Step{
			Change: rs.Change, Step: st.ID, Role: st.Role, Round: rs.Round,
			Agent: tmpl, Model: model, Effort: effort,
			Permissions: e.Config.PermissionArgs(st),
			Worktree:    ws.Path, State: ws.State, Base: rs.Base,
			Earlier: rs.Earlier(), Scope: rs.Scope, Term: e.term,
		}, opts)
		// The backend has had the adopted agent: it ends it on a stop.
		e.adopting = nil
		var endErr *agent.EndError
		if errors.As(err, &endErr) {
			return e.unwatched(ws, rs, endErr)
		}
		rs.AgentPid, rs.Terminal, rs.AgentState = 0, nil, ""
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

// stopAdopted ends the live agent a resumed run adopted but has not yet
// handed to its step's backend, and knows it is gone, before it records
// the run stopped. An agent whose terminal will not end leaves the run as
// found: running, with its terminal and slot, for stop or resume.
func (e *Engine) stopAdopted(ws Workspace, rs *Run, adopt *adoption, reason string) (*Run, error) {
	e.adopting = nil
	ectx, ecancel := context.WithTimeout(context.Background(), 2*time.Minute)
	err := adopt.Backend.EndAgent(ectx, adopt.Name)
	ecancel()
	if err != nil {
		return e.unwatched(ws, rs, &agent.EndError{Name: adopt.Name, Err: err})
	}
	return e.finish(ws, rs, Stopped, reason)
}

// unwatched leaves the run as a run process that died would: running,
// with its terminal and slot recorded, because its agent's terminal would
// not end and may still be live. Nothing is recorded stopped and the
// workflow does not move on until the terminal is known to have ended:
// tuckerbox stop takes the lock and tries again, and resume adopts the
// agent or ends it (formal/terminals.tla, StoppedMeansNoAgent).
func (e *Engine) unwatched(ws Workspace, rs *Run, err *agent.EndError) (*Run, error) {
	rs.State, rs.Reason = Running, err.Error()
	rs.Pid, rs.AgentPid = 0, 0
	if rs.Terminal == nil {
		rs.Terminal = &TerminalRef{Name: err.Name}
	}
	e.logf(rs.Change, "%v; leaving the run for tuckerbox stop or resume", err)
	if serr := e.save(ws, rs); serr != nil {
		return rs, serr
	}
	return rs, err
}

// finish records the run's final state for this process. The caller's
// deferred releases then free the slot and the change lock.
func (e *Engine) finish(ws Workspace, rs *Run, s State, reason string) (*Run, error) {
	rs.State, rs.Reason = s, reason
	rs.Pid, rs.AgentPid = 0, 0
	rs.Slot, rs.Terminal, rs.AgentState = nil, nil, ""
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
