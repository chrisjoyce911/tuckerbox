package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// Agent states a terminal step reports through Options.OnAgentState.
const (
	// AgentWorking: the agent is at work on its turn.
	AgentWorking = "working"
	// AgentWaiting: a turn ended without a valid result; the agent waits
	// for the person to type to it.
	AgentWaiting = "waiting for you"
)

// Terminal labels Tuckerbox sets on an agent's terminal.
const (
	LabelSpace  = "herdr.space"
	LabelTab    = "herdr.tab"
	LabelChange = "tuckerbox.change"
	LabelStep   = "tuckerbox.step"
	LabelRound  = "tuckerbox.round"
	LabelRun    = "tuckerbox.run"
)

// TermInfo is what the terminal backend needs beyond a headless step.
type TermInfo struct {
	// Name is the change's terminal name (TerminalName): every step and
	// round of a change uses it, so herdr-core's refusal of a second live
	// terminal of that name keeps one live agent per change.
	Name string
	// Space is the herdr.space label: the repository's name.
	Space string
	// RunID identifies the run, for the tuckerbox.run label.
	RunID string
	// Common is the repository's common Tuckerbox directory, where the
	// slot locks are; Slot is the run's parallel slot, whose agent lock
	// tuckerbox agent-hold takes in the terminal.
	Common string
	Slot   int
	// Owner is the run process's claim on its slot (runs.SlotOwner):
	// agent-hold starts the agent only while the slot's owner file still
	// names it, so an agent-hold that starts after its run process died
	// and the slot went to another change does not launch.
	Owner string
}

// EndError means an agent's terminal was asked to end and did not, or
// could not be asked: the agent may still be live. The step's outcome is
// not final, and the run must keep what it needs to find the terminal
// again (its name and slot) rather than record stopped or move on.
type EndError struct {
	Name string
	Err  error
}

func (e *EndError) Error() string {
	return fmt.Sprintf("the agent's terminal %s did not end: %v", e.Name, e.Err)
}

func (e *EndError) Unwrap() error { return e.Err }

// TerminalName is the herdr-core name of a change's agent terminal: "tb-"
// and the first 12 hex digits of a SHA-256 of the repository's identity
// (its normalised remote, or its path without one) and the change id.
// herdr-core names are at most 32 characters of a-z, 0-9, - and _.
func TerminalName(repoID, change string) string {
	sum := sha256.Sum256([]byte(repoID + "\x00" + change))
	return "tb-" + hex.EncodeToString(sum[:])[:12]
}

// MatchesStep reports whether a terminal's labels name this change, step
// and round: such a live terminal is adopted rather than replaced.
func MatchesStep(t *HCTerminal, change, step string, round int) bool {
	return t != nil && t.Labels[LabelChange] == change && t.Labels[LabelStep] == step &&
		t.Labels[LabelRound] == strconv.Itoa(round)
}

// Terminal is the terminal backend: each step's agent runs in its
// interactive mode in a herdr-core terminal, which a person can watch and
// type into, and which outlives Tuckerbox's run process.
type Terminal struct {
	HC HC
	// Exe is the tuckerbox program the terminal runs agent-hold with, and
	// the turn hook runs agent-turn with.
	Exe string
}

// Name returns "terminal".
func (Terminal) Name() string { return config.BackendTerminal }

// LiveAgent returns the live terminal called name, or nil.
func (t Terminal) LiveAgent(ctx context.Context, name string) (*HCTerminal, error) {
	term, err := t.HC.Get(ctx, name)
	if err != nil || !term.Live() {
		return nil, err
	}
	return term, nil
}

// EndAgent ends the terminal called name and waits until it is gone.
func (t Terminal) EndAgent(ctx context.Context, name string) error {
	return t.HC.End(ctx, name, endTimeout)
}

// Adopter is a backend whose agents outlive the run process: their
// terminals can be found by name, adopted and ended.
type Adopter interface {
	Backend
	LiveAgent(ctx context.Context, name string) (*HCTerminal, error)
	EndAgent(ctx context.Context, name string) error
}

// endTimeout bounds the wait for an ended terminal to go.
const endTimeout = 30 * time.Second

// terminalPrompt is the interactive agent's first message.
func terminalPrompt(briefFile string) string {
	return "Read the Tuckerbox brief at " + briefFile + " and follow it. " +
		"It ends with the result JSON your final message must contain. " +
		"If you need a decision from the person watching, ask it and end your turn; they answer here."
}

// HoldArgv is the command a terminal runs: tuckerbox agent-hold, which
// takes the slot's agent lock and runs the agent with env as its child.
func HoldArgv(exe string, ti TermInfo, env []string, inner []string) []string {
	argv := []string{exe, "agent-hold", "--slot", strconv.Itoa(ti.Slot)}
	if ti.Common != "" {
		argv = append(argv, "--common", ti.Common)
	}
	if ti.Owner != "" {
		argv = append(argv, "--owner", ti.Owner)
	}
	for _, e := range env {
		argv = append(argv, "--env", e)
	}
	return append(append(argv, "--"), inner...)
}

// stepEnv is the TUCKERBOX_* environment of a step's agent.
func stepEnv(s Step, briefPath, resultPath string) []string {
	env := []string{
		"TUCKERBOX_CHANGE=" + s.Change,
		"TUCKERBOX_STEP=" + s.Step,
		"TUCKERBOX_ROUND=" + strconv.Itoa(s.Round),
		"TUCKERBOX_BRIEF=" + briefPath,
		"TUCKERBOX_RESULT=" + resultPath,
	}
	return append(env, s.Env...)
}

// Run runs the step's agent in a terminal and waits for its result. The
// caller holds the change lock (formal/terminals.tla, Launch).
//
// A live terminal with the change's name is adopted when its labels name
// this step and round (the run process that started it died); otherwise
// it is ended, and waited for, before the new one starts. The new
// terminal runs tuckerbox agent-hold, which holds the run's slot's agent
// lock for as long as the agent lives (formal/liveslots.tla).
//
// At the end of each turn the CLI's hook writes its final message to the
// step's last-message file. A valid result there, or in the result file,
// is accepted; the agent's program is ended after ResultGrace and the
// terminal's history appended to the step's log. A turn without one
// leaves the step running with the agent waiting for the person
// (AgentWaiting). Cancelling ctx (stop) ends the terminal. A terminal
// that would not end, on stop or after a result, gives an *EndError: the
// agent may live on, so the step is not over. A terminal
// that ends without a valid result gives an Outcome without one.
func (t Terminal) Run(ctx context.Context, s Step, opts Options) (Outcome, error) {
	opts = withDefaults(opts)
	var o Outcome
	if err := statedir.CheckID("step", s.Step); err != nil {
		return o, err
	}
	if s.Round < 1 {
		return o, fmt.Errorf("step %s: round must be 1 or more, got %d", s.Step, s.Round)
	}
	if s.State == "" || s.Worktree == "" || s.Term.Name == "" {
		return o, errors.New("step needs a worktree, a state directory and a terminal name")
	}
	if !s.Agent.HasInteractive() {
		return o, fmt.Errorf("step %s: agent %s has no interactive command", s.Step, s.Agent.Name)
	}
	if t.Exe == "" {
		return o, errors.New("the terminal backend needs the tuckerbox program")
	}
	if err := s.State.Ensure(); err != nil {
		return o, err
	}
	o.BriefPath = s.State.Brief(s.Step, s.Round)
	o.ResultPath = s.State.Result(s.Step, s.Round)
	o.LogPath = s.State.Log(s.Step)
	o.MessagePath = s.State.LastMessage(s.Step, s.Round)
	want := Expect{Change: s.Change, Step: s.Step, Round: s.Round}
	name := s.Term.Name

	logf, err := openLog(o.LogPath)
	if err != nil {
		return o, fmt.Errorf("step %s: log: %w", s.Step, err)
	}
	defer logf.Close()
	log := io.Writer(logf)

	cur, err := t.HC.Get(ctx, name)
	if err != nil {
		return o, fmt.Errorf("step %s: looking for terminal %s: %w", s.Step, name, err)
	}
	adopt := cur.Live() && MatchesStep(cur, s.Change, s.Step, s.Round)
	if cur.Live() && !adopt {
		logLine(log, "step %s round %d: ending terminal %s left from step %s round %s",
			s.Step, s.Round, name, cur.Labels[LabelStep], cur.Labels[LabelRound])
		if err := t.HC.End(ctx, name, opts.EndTimeout); err != nil {
			return o, fmt.Errorf("step %s: %w", s.Step, &EndError{Name: name, Err: err})
		}
	}

	var addr string
	if adopt {
		addr = cur.Address
		logLine(log, "step %s round %d: adopted live terminal %s (%s)", s.Step, s.Round, name, addr)
	} else {
		if err := t.prepare(s, &o); err != nil {
			return o, err
		}
		hook := TurnHookArgv(t.Exe, s.State, s.Step, s.Round, s.Agent.Hook)
		inner, err := s.Agent.ExpandInteractive(Values{
			Brief: terminalPrompt(o.BriefPath), BriefFile: o.BriefPath, Result: o.ResultPath,
			LastMessage: o.MessagePath, Workdir: s.Worktree, Model: s.Model, Effort: s.Effort,
			Permissions: s.Permissions, TurnHook: hook,
		})
		if err != nil {
			return o, err
		}
		argv := HoldArgv(t.Exe, s.Term, stepEnv(s, o.BriefPath, o.ResultPath), inner)
		o.Argv = argv
		start := HCStart{Name: name, Cwd: s.Worktree, Argv: argv, Labels: [][2]string{
			{LabelSpace, s.Term.Space},
			{LabelTab, s.Change + " · " + s.Step},
			{LabelChange, s.Change},
			{LabelStep, s.Step},
			{LabelRound, strconv.Itoa(s.Round)},
			{LabelRun, s.Term.RunID},
		}}
		// Launch intent: the run's state names the terminal before it can
		// start, so a run process that dies while hc starts it leaves a
		// terminal that resume can adopt and stop can end.
		if opts.OnTerminal != nil {
			opts.OnTerminal(name, "")
		}
		logLine(log, "step %s round %d started in terminal %s: %s", s.Step, s.Round, name, strings.Join(quoteArgs(argv), " "))
		term, err := t.HC.Start(ctx, start)
		if err != nil {
			got, rerr := t.reconcileStart(ctx, s, opts, log, err)
			switch {
			case rerr != nil:
				o.ExitCode = -1
				return o, fmt.Errorf("step %s: %w", s.Step, rerr)
			case got == nil && ctx.Err() != nil:
				o.Stopped, o.ExitCode = true, -1
				logLine(log, "step %s round %d: stopped while its terminal was starting; no agent is left", s.Step, s.Round)
				return o, nil
			case got == nil:
				o.ExitCode = -1
				return o, fmt.Errorf("step %s: starting agent %s in terminal %s: %w", s.Step, s.Agent.Name, name, err)
			}
			term = *got
		}
		addr = term.Address
	}
	if opts.OnTerminal != nil {
		opts.OnTerminal(name, addr)
	}
	agentState := ""
	setState := func(st string) {
		if st != agentState {
			agentState = st
			if opts.OnAgentState != nil {
				opts.OnAgentState(st)
			}
		}
	}
	setState(AgentWorking)

	w := watcher{path: o.ResultPath, want: want, log: log}
	mw := turnWatcher{path: o.MessagePath, want: want, log: log}
	accept := func() bool {
		if o.Result != nil {
			return false
		}
		if r := w.check(); r != nil {
			o.Result, o.Source = r, "result file"
			return true
		}
		r, ended := mw.check()
		switch {
		case r != nil:
			o.Result, o.Source = r, messageLabel
			logLine(log, "accepted result from the final message: %s, %s", r.Status, oneLine(r.Summary))
			if err := writeResult(o.ResultPath, *r); err != nil {
				logLine(log, "writing result file %s: %v", o.ResultPath, err)
			}
			return true
		case ended:
			setState(AgentWaiting)
		}
		return false
	}

	// end ends the terminal and waits until it is gone. A terminal that
	// would not end makes Run return an *EndError: the agent may be live.
	var endErr error
	end := func() {
		ectx, cancel := context.WithTimeout(context.Background(), opts.EndTimeout+5*time.Second)
		defer cancel()
		if err := t.HC.End(ectx, name, opts.EndTimeout); err != nil {
			logLine(log, "step %s round %d: ending terminal %s: %v", s.Step, s.Round, name, err)
			endErr = &EndError{Name: name, Err: err}
		}
	}
	tick := time.NewTicker(opts.Poll)
	defer tick.Stop()
	var graceC <-chan time.Time
	lastLook := time.Time{}
loop:
	for {
		select {
		case <-ctx.Done():
			o.Stopped = true
			logLine(log, "step %s round %d: stopping agent: ending terminal %s", s.Step, s.Round, name)
			end()
			break loop
		case <-graceC:
			logLine(log, "step %s round %d: agent still running %s after its result; ending terminal %s",
				s.Step, s.Round, opts.ResultGrace, name)
			end()
			break loop
		case <-tick.C:
			if accept() {
				graceC = time.After(opts.ResultGrace)
			}
			if time.Since(lastLook) < opts.TermPoll {
				continue
			}
			lastLook = time.Now()
			term, err := t.HC.Get(ctx, name)
			switch {
			case err != nil:
				if ctx.Err() == nil {
					logLine(log, "step %s round %d: looking at terminal %s: %v", s.Step, s.Round, name, err)
				}
			case !term.Live():
				logLine(log, "step %s round %d: the agent's terminal %s ended", s.Step, s.Round, name)
				break loop
			case agentState == AgentWaiting && term.Status != nil && *term.Status == "working":
				setState(AgentWorking)
			}
		}
	}
	// The agent may have reported just before its terminal ended.
	if !o.Stopped || o.Result == nil {
		accept()
	}
	o.Ignored = append(w.ignored, mw.ignored...)
	o.ExitCode = -1
	if term, err := t.HC.Get(context.Background(), name); err == nil && term != nil && term.Exit != nil {
		o.ExitCode = *term.Exit
	}
	if endErr != nil {
		logLine(log, "step %s round %d: the agent's terminal %s may still be live; the step is not finished", s.Step, s.Round, name)
		return o, fmt.Errorf("step %s: %w", s.Step, endErr)
	}
	t.appendHistory(log, s, name)
	logLine(log, "step %s round %d finished: exit %d, status %s", s.Step, s.Round, o.ExitCode, o.Status())
	if o.Result == nil {
		o.Tail, _ = TailLog(o.LogPath, opts.TailBytes)
	}
	return o, nil
}

// startSettle is how long reconcileStart keeps looking for a terminal an
// unanswered hc run may still be starting.
const startSettle = 1500 * time.Millisecond

// reconcileStart decides what an hc run that gave startErr left behind: an
// error does not prove that no terminal started (the run was cancelled
// after herdr-core launched it, or its answer was lost or unreadable). It
// looks with its own context, since ctx may be cancelled.
//
// It returns the terminal to watch when this start's terminal exists and
// the run goes on (ctx is live); nil when no terminal of this start is
// live, ending it first when ctx is cancelled; and an *EndError when it
// cannot tell or the terminal would not end, so the run keeps the name
// and slot that find the agent again.
func (t Terminal) reconcileStart(ctx context.Context, s Step, opts Options, log io.Writer, startErr error) (*HCTerminal, error) {
	name := s.Term.Name
	logLine(log, "step %s round %d: hc run gave an error: %v; checking whether terminal %s started", s.Step, s.Round, startErr, name)
	bg, cancel := context.WithTimeout(context.Background(), opts.EndTimeout+startSettle+10*time.Second)
	defer cancel()
	// herdr-core answered with an error of its own: it refused the start.
	// Otherwise the start may still be on its way: look for a while.
	var he *HCError
	definite := errors.As(startErr, &he) && he.Kind != "" && ctx.Err() == nil
	deadline := time.Now().Add(startSettle)
	for {
		cur, err := t.HC.Get(bg, name)
		if err != nil {
			logLine(log, "step %s round %d: cannot tell whether terminal %s started: %v", s.Step, s.Round, name, err)
			return nil, &EndError{Name: name, Err: fmt.Errorf("starting it gave %v, and looking for it gave %w", startErr, err)}
		}
		mine := cur != nil && MatchesStep(cur, s.Change, s.Step, s.Round) && cur.Labels[LabelRun] == s.Term.RunID
		if mine && ctx.Err() == nil {
			logLine(log, "step %s round %d: terminal %s did start (%s); watching it", s.Step, s.Round, name, cur.Address)
			return cur, nil
		}
		if cur.Live() {
			// Stopped while starting (or some other live terminal, which
			// under the change lock cannot be another run's): end it and
			// make sure it is gone before the run records anything.
			logLine(log, "step %s round %d: ending terminal %s, which started as the run stopped", s.Step, s.Round, name)
			if err := t.HC.End(bg, name, opts.EndTimeout); err != nil {
				return nil, &EndError{Name: name, Err: err}
			}
			return nil, nil
		}
		if mine || definite || time.Now().After(deadline) {
			logLine(log, "step %s round %d: terminal did not start: %v", s.Step, s.Round, startErr)
			return nil, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// prepare writes a new terminal's brief and clears the round's leftover
// result and final message, as Run does for a headless agent.
func (t Terminal) prepare(s Step, o *Outcome) error {
	brief := Brief{
		Change: s.Change, Step: s.Step, Role: s.Role, Round: s.Round,
		Root: s.Worktree, ResultPath: o.ResultPath, Base: s.Base, Earlier: s.Earlier,
		Scope: s.Scope, ShippingFeedback: s.ShippingFeedback,
	}
	text, err := brief.Render()
	if err == nil {
		err = writeBrief(o.BriefPath, text)
	}
	if err != nil {
		return fmt.Errorf("step %s: brief: %w", s.Step, err)
	}
	if _, err := os.Stat(o.ResultPath); err == nil {
		old := o.ResultPath + ".old-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		if err := os.Rename(o.ResultPath, old); err != nil {
			return fmt.Errorf("step %s: moving old result aside: %w", s.Step, err)
		}
	}
	if err := os.Remove(o.MessagePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("step %s: removing old final message: %w", s.Step, err)
	}
	return nil
}

// appendHistory appends the terminal's history to the step's log, so the
// step's record does not depend on herdr-core keeping finished terminals.
func (t Terminal) appendHistory(log io.Writer, s Step, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := t.HC.Read(ctx, name)
	if err != nil {
		logLine(log, "step %s round %d: reading terminal %s: %v", s.Step, s.Round, name, err)
		return
	}
	logLine(log, "step %s round %d: history of terminal %s:", s.Step, s.Round, name)
	_, _ = log.Write(out)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		_, _ = io.WriteString(log, "\n")
	}
}

// turnWatcher reads the last-message file the turn hook writes after each
// turn.
type turnWatcher struct {
	path     string
	want     Expect
	log      io.Writer
	stamp    string
	text     string
	ignored  []string
	accepted *Result
}

// check reads the file when it changed since the last look. It returns
// the result when the turn's message holds a valid one, and ended when a
// turn ended without one.
func (m *turnWatcher) check() (r *Result, ended bool) {
	if m.accepted != nil {
		return m.accepted, false
	}
	fi, err := os.Stat(m.path)
	if err != nil {
		return nil, false
	}
	stamp := fmt.Sprintf("%d/%d", fi.Size(), fi.ModTime().UnixNano())
	if stamp == m.stamp {
		return nil, false
	}
	msg, ok, err := ReadLastMessage(m.path)
	if err != nil || !ok {
		return nil, false
	}
	m.stamp, m.text = stamp, msg
	res, err := MessageResult(msg, m.want)
	if err != nil {
		m.ignored = append(m.ignored, err.Error())
		logLine(m.log, "turn ended without a valid result (%v); waiting for the person", err)
		return nil, true
	}
	m.accepted = &res
	return &res, false
}
