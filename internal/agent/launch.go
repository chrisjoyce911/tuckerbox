package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/chrisjoyce911/tuckerbox/internal/scope"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// Step is everything needed to run one step's agent once.
type Step struct {
	ShippingFeedback bool
	Change           string
	Step             string
	Role             string
	Round            int
	// Agent is the resolved agent definition (see Resolve).
	Agent  Template
	Model  string
	Effort string
	// Permissions are the flags of the step's machine permission profile,
	// already resolved and checked against the repository's remote. Nil
	// means no profile: the agent runs with its own defaults.
	Permissions []string
	// Worktree is the change's git worktree, the agent's working directory.
	Worktree string
	// State is the worktree's state directory (statedir.Resolve).
	State statedir.Dir
	// Base is the ref the branch started from, if known; it goes in the brief.
	Base string
	// Earlier are accepted results from earlier rounds whose findings this
	// round must address.
	Earlier []Result
	// Env is extra environment for the agent, as KEY=value.
	Env []string
	// Scope is the run's task scope, or nil; it goes in the brief.
	Scope *scope.Scope
	// Term is what the terminal backend needs; headless ignores it.
	Term TermInfo
}

// Options tune Run. The zero value is ready to use.
type Options struct {
	// Poll is how often the result file is checked while the agent runs
	// (default 500ms).
	Poll time.Duration
	// ResultGrace is how long the agent may keep running after it wrote a
	// valid result before it is stopped (default 2 minutes).
	ResultGrace time.Duration
	// StopGrace is how long the agent has to exit after it is asked to stop,
	// before it is killed (default 10 seconds).
	StopGrace time.Duration
	// TailBytes is how much output is kept for a step without a result
	// (default 4 KiB).
	TailBytes int64
	// OnStart is called with the agent's pid once it has started
	// (headless).
	OnStart func(pid int)
	// OnTerminal is called with the terminal's name and address once the
	// agent's terminal is started or adopted (terminal backend).
	OnTerminal func(name, address string)
	// OnAgentState is called when the agent's state changes: AgentWorking
	// or AgentWaiting (terminal backend).
	OnAgentState func(state string)
	// TermPoll is how often the terminal backend asks herdr-core whether
	// the agent's terminal still lives (default 2 seconds).
	TermPoll time.Duration
}

// Outcome is what happened when a step's agent ran.
type Outcome struct {
	// Result is the accepted result, or nil when the agent left no valid
	// result for this step and round. The step is then failed.
	Result *Result
	// ExitCode is the agent's exit status (-1 if it was killed by a signal
	// or did not start).
	ExitCode int
	// Stopped is true when ctx ended before the agent finished. Result is
	// still set if the agent wrote a valid one before it was stopped.
	Stopped bool
	// Ignored records each stale or malformed result seen, in a result file
	// or the final message, and why.
	Ignored []string
	// Source says where Result came from: "final message" or "result file".
	Source string
	// Tail is the end of the step's log, set when Result is nil.
	Tail string
	// Argv is the command line that was run, with the brief's text shown as
	// "<brief: N chars, <file>>".
	Argv []string
	// BriefPath, ResultPath and LogPath are the step's files. MessagePath is
	// the {lastmessage} file, set only when the agent's template passes it.
	BriefPath, ResultPath, MessagePath, LogPath string
}

// Status is the step's outcome: the agent's reported status, or failed
// when there is no valid result.
func (o Outcome) Status() Status {
	if o.Result == nil {
		return Failed
	}
	return o.Result.Status
}

// FailureReason describes a step without a valid result, for run state.
func (o Outcome) FailureReason() string {
	if o.Result != nil {
		return ""
	}
	if o.Stopped {
		return "stopped before the agent wrote a result"
	}
	msg := fmt.Sprintf("agent exited with status %d without a valid result", o.ExitCode)
	if len(o.Ignored) > 0 {
		msg += " (ignored: " + strings.Join(o.Ignored, "; ") + ")"
	}
	return msg
}

// readBriefAt is what a custom agent gets as {brief} when the brief is too
// long to pass inline.
func readBriefAt(file string) string {
	return "The Tuckerbox brief is too long to pass here. Read it at " + file +
		" and follow it; it ends with the result JSON your final message must contain. " +
		"If you can't read that file, end with a result whose status is blocked and whose summary names the file."
}

// briefLabel stands in for the brief's text in logs and run state.
func briefLabel(text, file string) string {
	return fmt.Sprintf("<brief: %d chars, %s>", utf8.RuneCountInString(text), file)
}

// command expands the step's command line with the brief inline, or, when
// that would be longer than MaxInlineCommand as a Windows command line, with
// the brief on standard input (built-ins) or an instruction to read the brief
// file (other agents). It also returns the command as logged, with the brief
// shown as briefLabel, whether the brief goes on standard input, and a note
// for the log when the brief could not be passed inline.
func command(t Template, v Values) (argv, shown []string, stdin bool, note string, err error) {
	text := v.Brief
	argv, err = t.Expand(v)
	if err != nil {
		return nil, nil, false, "", err
	}
	if n := windowsCmdLineLen(argv); n > MaxInlineCommand {
		if t.Long.UsesStdin() {
			stdin, v.BriefOnStdin = true, true
			note = fmt.Sprintf("brief too long to pass inline (command line %d characters, limit %d); sending it on standard input",
				n, MaxInlineCommand)
		} else {
			v.Brief = readBriefAt(v.BriefFile)
			note = fmt.Sprintf("brief too long to pass inline (command line %d characters, limit %d); the agent is told to read %s",
				n, MaxInlineCommand, v.BriefFile)
		}
		if argv, err = t.Expand(v); err != nil {
			return nil, nil, false, "", err
		}
	}
	if v.Brief == text {
		v.Brief = briefLabel(text, v.BriefFile)
	}
	shown, err = t.Expand(v)
	return argv, shown, stdin, note, err
}

// Run writes the step's brief, starts its agent headless in the worktree with
// output appended to the step's log, and waits for it.
//
// The agent reports by ending with its result JSON as its final message (see
// FinalMessage); Run then writes the result file itself, so the agent needs
// no write access outside its worktree. A result file the agent writes
// itself is also accepted, and wins when both exist and differ (logged).
// Only a well-formed result naming this change, step and round is accepted;
// stale and malformed results are logged, recorded in Outcome.Ignored and
// waited past.
//
// Cancelling ctx stops the agent (stop): it is asked to exit, then killed
// after StopGrace. An error is returned only when the agent could not be
// started; an agent that runs and fails is reported in the Outcome.
func Run(ctx context.Context, s Step, opts Options) (Outcome, error) {
	opts = withDefaults(opts)
	var o Outcome
	if err := statedir.CheckID("step", s.Step); err != nil {
		return o, err
	}
	if s.Round < 1 {
		return o, fmt.Errorf("step %s: round must be 1 or more, got %d", s.Step, s.Round)
	}
	if s.State == "" || s.Worktree == "" {
		return o, errors.New("step needs a worktree and a state directory")
	}
	if err := s.State.Ensure(); err != nil {
		return o, err
	}
	o.BriefPath = s.State.Brief(s.Step, s.Round)
	o.ResultPath = s.State.Result(s.Step, s.Round)
	o.LogPath = s.State.Log(s.Step)
	if s.Agent.UsesLastMessage() {
		o.MessagePath = s.State.LastMessage(s.Step, s.Round)
	}
	want := Expect{Change: s.Change, Step: s.Step, Round: s.Round}

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
		return o, fmt.Errorf("step %s: brief: %w", s.Step, err)
	}
	// A result file already at this round's path was not written by this
	// run of the step; move it aside so it can't be taken as fresh.
	if _, err := os.Stat(o.ResultPath); err == nil {
		old := o.ResultPath + ".old-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		if err := os.Rename(o.ResultPath, old); err != nil {
			return o, fmt.Errorf("step %s: moving old result aside: %w", s.Step, err)
		}
	}
	if o.MessagePath != "" {
		if err := os.Remove(o.MessagePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return o, fmt.Errorf("step %s: removing old final message: %w", s.Step, err)
		}
	}

	argv, shown, briefOnStdin, note, err := command(s.Agent, Values{
		Brief: text, BriefFile: o.BriefPath, Result: o.ResultPath, LastMessage: o.MessagePath,
		Workdir: s.Worktree, Model: s.Model, Effort: s.Effort, Permissions: s.Permissions,
	})
	if err != nil {
		return o, err
	}
	o.Argv = shown

	logf, err := openLog(o.LogPath)
	if err != nil {
		return o, fmt.Errorf("step %s: log: %w", s.Step, err)
	}
	defer logf.Close()
	// The agent's stdout and stderr are copied into the log by two
	// goroutines; stdout is also kept, to find the final message in.
	var logMu sync.Mutex
	log := lockedWriter{mu: &logMu, w: logf}
	stdout := &tailBuffer{max: maxResultSize + 64<<10}
	if note != "" {
		logLine(log, "step %s round %d: %s", s.Step, s.Round, note)
	}
	logLine(log, "step %s round %d started: %s", s.Step, s.Round, strings.Join(quoteArgs(shown), " "))

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = s.Worktree
	var stdin io.WriteCloser
	if briefOnStdin {
		if stdin, err = cmd.StdinPipe(); err != nil {
			return o, fmt.Errorf("step %s: standard input: %w", s.Step, err)
		}
	}
	cmd.Stdout = io.MultiWriter(log, stdout)
	cmd.Stderr = log
	// Children the agent leaves behind may hold its output open; don't let
	// them keep the step waiting once the agent itself has exited.
	cmd.WaitDelay = opts.StopGrace
	cmd.Env = append(os.Environ(),
		"TUCKERBOX_CHANGE="+s.Change,
		"TUCKERBOX_STEP="+s.Step,
		"TUCKERBOX_ROUND="+strconv.Itoa(s.Round),
		"TUCKERBOX_BRIEF="+o.BriefPath,
		"TUCKERBOX_RESULT="+o.ResultPath,
	)
	cmd.Env = append(cmd.Env, s.Env...)
	setProcAttr(cmd)
	if err := cmd.Start(); err != nil {
		logLine(log, "step %s round %d: agent did not start: %v", s.Step, s.Round, err)
		o.ExitCode = -1
		return o, fmt.Errorf("step %s: starting agent %s: %w", s.Step, s.Agent.Name, err)
	}
	if stdin != nil {
		// Written from a goroutine so an agent that reads slowly can't hold
		// up the step; closed after, so the agent sees end of file. An agent
		// that exits without reading it all only ends the write early.
		go func() {
			_, _ = io.WriteString(stdin, text)
			_ = stdin.Close()
		}()
	}
	if opts.OnStart != nil {
		opts.OnStart(cmd.Process.Pid)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	w := watcher{path: o.ResultPath, want: want, log: log}
	var waitErr error
	var graceC <-chan time.Time
	tick := time.NewTicker(opts.Poll)
	defer tick.Stop()
loop:
	for {
		select {
		case waitErr = <-exited:
			break loop
		case <-ctx.Done():
			o.Stopped = true
			logLine(log, "step %s round %d: stopping agent", s.Step, s.Round)
			waitErr = stop(cmd, exited, opts.StopGrace)
			break loop
		case <-graceC:
			logLine(log, "step %s round %d: agent still running %s after its result; stopping it",
				s.Step, s.Round, opts.ResultGrace)
			waitErr = stop(cmd, exited, opts.StopGrace)
			break loop
		case <-tick.C:
			if graceC == nil && w.check() != nil {
				graceC = time.After(opts.ResultGrace)
			}
		}
	}
	o.ExitCode = exitCode(cmd, waitErr)
	// The agent may write its result just before it exits; read once more.
	w.check()
	o.Result = w.accepted
	o.Ignored = w.ignored
	if o.Result != nil {
		o.Source = "result file"
	}
	if !o.Stopped {
		o.fromMessage(want, stdout.Bytes(), log)
	}
	logLine(log, "step %s round %d finished: exit %d, status %s", s.Step, s.Round, o.ExitCode, o.Status())
	if o.Result == nil {
		o.Tail, _ = TailLog(o.LogPath, opts.TailBytes)
	}
	return o, nil
}

func withDefaults(o Options) Options {
	if o.Poll <= 0 {
		o.Poll = 500 * time.Millisecond
	}
	if o.ResultGrace <= 0 {
		o.ResultGrace = 2 * time.Minute
	}
	if o.StopGrace <= 0 {
		o.StopGrace = 10 * time.Second
	}
	if o.TailBytes <= 0 {
		o.TailBytes = 4 << 10
	}
	if o.TermPoll <= 0 {
		o.TermPoll = 2 * time.Second
	}
	return o
}

// watcher polls a result file, accepting only the expected step and round.
type watcher struct {
	path     string
	want     Expect
	log      io.Writer
	accepted *Result
	ignored  []string
	lastSeen string // size and mtime of the last ignored file, to log it once
}

func (w *watcher) check() *Result {
	if w.accepted != nil {
		return w.accepted
	}
	fi, err := os.Stat(w.path)
	if err != nil {
		return nil
	}
	stamp := fmt.Sprintf("%d/%d", fi.Size(), fi.ModTime().UnixNano())
	if stamp == w.lastSeen {
		return nil
	}
	r, err := ReadResult(w.path, w.want)
	switch {
	case err == nil:
		w.accepted = &r
		logLine(w.log, "accepted result: %s, %s", r.Status, oneLine(r.Summary))
	case IsIgnored(err):
		w.lastSeen = stamp
		w.ignored = append(w.ignored, err.Error())
		logLine(w.log, "ignored result file: %v", err)
	case errors.Is(err, ErrNoResult):
	default:
		logLine(w.log, "reading result file: %v", err)
	}
	return w.accepted
}

// stop asks the agent to exit, kills it after grace, and returns its wait
// error.
func stop(cmd *exec.Cmd, exited <-chan error, grace time.Duration) error {
	_ = interrupt(cmd)
	select {
	case err := <-exited:
		return err
	case <-time.After(grace):
	}
	_ = kill(cmd)
	return <-exited
}

func exitCode(cmd *exec.Cmd, err error) int {
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	if err != nil {
		return -1
	}
	return 0
}

func logLine(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, "\n[tuckerbox %s] %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

func quoteArgs(argv []string) []string {
	out := make([]string, len(argv))
	for i, a := range argv {
		if a == "" || strings.ContainsAny(a, " \t\"'") {
			out[i] = strconv.Quote(a)
		} else {
			out[i] = a
		}
	}
	return out
}

// fromMessage reads the agent's final message, from its {lastmessage} file or
// its stdout, and reconciles it with any result file the agent wrote.
func (o *Outcome) fromMessage(want Expect, stdout []byte, log io.Writer) {
	var msg string
	var ok bool
	if o.MessagePath != "" {
		var err error
		msg, ok, err = ReadLastMessage(o.MessagePath)
		if err != nil {
			logLine(log, "reading final message: %v", err)
		}
	} else {
		msg, ok = FinalMessage(stdout)
	}
	if !ok {
		if o.Result == nil {
			logLine(log, "no final message from the agent")
		}
		return
	}
	r, err := MessageResult(msg, want)
	switch {
	case err != nil && o.Result != nil:
		// The result file stands; the message is only noted.
		logLine(log, "final message holds no valid result (%v); using the result file", err)
	case err != nil:
		o.Ignored = append(o.Ignored, err.Error())
		logLine(log, "ignored final message: %v", err)
	case o.Result != nil:
		if !sameResult(*o.Result, r) {
			logLine(log, "result file and final message differ; using the result file (%s, %s) over the final message (%s, %s)",
				o.Result.Status, oneLine(o.Result.Summary), r.Status, oneLine(r.Summary))
		}
	default:
		o.Result, o.Source = &r, messageLabel
		logLine(log, "accepted result from the final message: %s, %s", r.Status, oneLine(r.Summary))
		if err := writeResult(o.ResultPath, r); err != nil {
			logLine(log, "writing result file %s: %v", o.ResultPath, err)
		}
	}
}
