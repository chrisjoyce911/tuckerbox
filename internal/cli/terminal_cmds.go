package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// hc returns the herdr-core client, and false when hc is not on the PATH.
func (a App) hc() (agent.HC, bool) {
	look := a.DoctorEnv.LookPath
	if look == nil {
		look = exec.LookPath
	}
	p, err := look(agent.HerdrCore)
	if err != nil {
		return agent.HC{}, false
	}
	return agent.HC{Path: p}, true
}

// agentBackend is the backend runs use on this machine for the machine
// config value setting: the terminal backend, with this tuckerbox program
// for agent-hold and agent-turn, or headless. It fails when agent_backend
// is "terminal" without a working herdr-core.
func (a App) agentBackend(setting string) (agent.Backend, error) {
	c, err := a.chooseBackend(setting)
	if err != nil {
		return nil, err
	}
	if c.Backend != config.BackendTerminal {
		return agent.Headless{}, nil
	}
	hc, ok := a.hc()
	if !ok {
		return nil, errors.New("herdr-core's hc is not on the PATH")
	}
	exe, err := a.executable()
	if err != nil {
		return nil, fmt.Errorf("finding the tuckerbox program for the terminal backend: %w", err)
	}
	return agent.Terminal{HC: hc, Exe: exe}, nil
}

// agentTurn is the hidden command the agent CLIs' end-of-turn hooks run:
//
//	tuckerbox agent-turn [--claude|--codex] --state <dir> --step <s> --round <r> [payload]
//
// It writes the turn's final message to the step's last-message file,
// atomically, and nothing else. --claude reads a Stop hook payload on
// standard input, --codex a notify payload as the last argument; with
// neither it takes either, or plain text.
func (a App) agentTurn(args []string) int { return a.agentTurnFrom(args, os.Stdin) }

func (a App) agentTurnFrom(args []string, stdin io.Reader) int {
	var claude, codex bool
	var state, step, round string
	pos, err := parseArgs(args, map[string]*bool{"claude": &claude, "codex": &codex},
		map[string]*string{"state": &state, "step": &step, "round": &round})
	if err != nil || state == "" || step == "" || round == "" || (claude && codex) {
		return a.usageErr("agent-turn", "usage: tuckerbox agent-turn [--claude|--codex] --state <dir> --step <s> --round <r> [payload]")
	}
	n, err := strconv.Atoi(round)
	if err != nil || n < 1 {
		return a.usageErr("agent-turn", "--round must be a number from 1")
	}
	if err := statedir.CheckID("step", step); err != nil {
		return a.errf("agent-turn", "%v", err)
	}
	format := agent.TurnAuto
	switch {
	case claude:
		format = agent.TurnClaude
	case codex:
		format = agent.TurnCodex
	}
	msg, err := agent.TurnMessage(format, pos, stdin)
	if err != nil {
		// The turn still ended: an empty message tells the run it ended
		// without a result, and the step waits for the person.
		fmt.Fprintf(a.Stderr, "tuckerbox agent-turn: %v\n", err)
		msg = ""
	}
	if err := agent.WriteLastMessage(statedir.Dir(state).LastMessage(step, n), msg); err != nil {
		return a.errf("agent-turn", "%v", err)
	}
	return 0
}

// agentHold is the hidden command an agent's terminal runs:
//
//	tuckerbox agent-hold --slot <n> [--common <dir>] [--owner <token>] [--env K=V]... -- <argv>
//
// It takes slot n's agent lock, refusing if another agent holds it, runs
// argv as its child with the extra environment, and exits with its exit
// status. The OS frees the lock when it exits, so the slot stays taken
// exactly as long as the agent lives (formal/liveslots.tla).
//
// With --owner, once it holds the agent lock it starts the agent only if
// the slot's owner file still holds that token: the run that started the
// terminal still owns the slot. A late agent-hold whose run died and
// whose slot was given to another change refuses (runs.Slot).
func (a App) agentHold(args []string) int {
	usage := "usage: tuckerbox agent-hold --slot <n> [--common <dir>] [--owner <token>] [--env K=V]... -- <program> [args...]"
	slot := -1
	var common, owner string
	var env, argv []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--":
			argv = args[i+1:]
			i = len(args)
		case "--slot", "--common", "--owner", "--env":
			if i+1 >= len(args) {
				return a.usageErr("agent-hold", usage)
			}
			v := args[i+1]
			switch args[i] {
			case "--slot":
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 {
					return a.usageErr("agent-hold", "--slot must be a slot number")
				}
				slot = n
			case "--common":
				common = v
			case "--owner":
				owner = v
			case "--env":
				env = append(env, v)
			}
			i++
		default:
			return a.usageErr("agent-hold", usage)
		}
	}
	if slot < 0 || len(argv) == 0 {
		return a.usageErr("agent-hold", usage)
	}
	if common == "" {
		_, getwd := a.configEnv()
		wd, err := getwd()
		if err != nil {
			return a.errf("agent-hold", "%v", err)
		}
		repo, err := runs.OpenRepo(context.Background(), wd)
		if err != nil {
			return a.errf("agent-hold", "%v", err)
		}
		common = repo.Common
	}
	hold, err := runs.HoldAgentLock(common, slot, 2*time.Second)
	if errors.Is(err, runs.ErrAgentLockHeld) {
		return a.errf("agent-hold", "slot %d's agent lock is held by another agent; not starting %s", slot, argv[0])
	}
	if err != nil {
		return a.errf("agent-hold", "%v", err)
	}
	defer hold.Release()
	if owner != "" {
		if got := runs.SlotOwner(common, slot); got != owner {
			return a.errf("agent-hold", "slot %d was given to another run after this agent's run took it; not starting %s", slot, argv[0])
		}
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), env...)
	// Signals that end the terminal are passed on; an interrupt (Ctrl-C)
	// already reaches the agent from its terminal, so it is only caught.
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, holdSignals...)
	defer signal.Stop(sigs)
	if err := cmd.Start(); err != nil {
		return a.errf("agent-hold", "starting %s: %v", argv[0], err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for {
		select {
		case sig := <-sigs:
			if forwardSignal(sig) {
				_ = cmd.Process.Signal(sig)
			}
		case <-done:
			code := cmd.ProcessState.ExitCode()
			if code < 0 {
				code = 1
			}
			return code
		}
	}
}

// turnHookChecks are doctor's turn-hook self-checks, one per built-in
// agent CLI: the payload its hook gets, the flag that installs it, and the
// hook's name.
var turnHookChecks = []struct {
	cli, flag, hook string
	style           agent.HookStyle
}{
	{"claude", "--settings", "Stop hook", agent.HookClaude},
	{"codex", "--config", "notify hook", agent.HookCodex},
}

// turnHookDoctorLines are doctor's turn-hook self-checks for each
// installed agent CLI: agent-turn, as the CLI's hook runs it, must get a
// sample final message from a payload in the format Tuckerbox expects
// from that CLI, and the CLI's --help must list the flag the hook is
// passed with. Whether the installed CLI really runs the hook with that
// payload is only seen in a real turn, which costs a model call, so doctor
// says that part is unverified rather than claiming the hook works. They
// never fail doctor; a CLI whose hook format changed leaves terminal steps
// waiting for the person.
func (a App) turnHookDoctorLines() []string {
	look := a.DoctorEnv.LookPath
	if look == nil {
		look = exec.LookPath
	}
	var lines []string
	for _, c := range turnHookChecks {
		path, err := look(c.cli)
		if err != nil {
			continue
		}
		if why := a.turnHookProblem(path, c.cli, c.flag, c.style); why != "" {
			lines = append(lines, fmt.Sprintf("turn hook: %s: %s", c.cli, why))
			continue
		}
		lines = append(lines, fmt.Sprintf("turn hook: %s: unverified: agent-turn reads the %s payload Tuckerbox expects and %s --help lists %s, "+
			"but whether this %s runs the hook with that payload is only seen in a real turn (a step whose turn gives no message waits for you)",
			c.cli, c.hook, c.cli, c.flag, c.cli))
	}
	return lines
}

// turnHookProblem runs one CLI's self-check and says what failed, or "".
func (a App) turnHookProblem(path, cli, flag string, style agent.HookStyle) string {
	dir, err := os.MkdirTemp("", "tuckerbox-doctor-turn")
	if err != nil {
		return err.Error()
	}
	defer os.RemoveAll(dir)
	const want = "tuckerbox turn hook self-check"
	state := statedir.Dir(filepath.Join(dir, "state"))
	hook := agent.TurnHookArgv("tuckerbox", state, "doctor", 1, style)
	args, stdin := hook[2:], ""
	switch style {
	case agent.HookClaude:
		transcript := filepath.Join(dir, "transcript.jsonl")
		line, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{
			"id": "m", "role": "assistant", "content": []map[string]string{{"type": "text", "text": want}}}})
		if err := os.WriteFile(transcript, append(line, '\n'), 0o644); err != nil {
			return err.Error()
		}
		payload, _ := json.Marshal(map[string]string{"hook_event_name": "Stop", "transcript_path": transcript})
		stdin = string(payload)
	case agent.HookCodex:
		payload, _ := json.Marshal(map[string]string{"type": "agent-turn-complete", "last-assistant-message": want})
		args = append(args, string(payload))
	}
	var errOut bytes.Buffer
	quiet := a
	quiet.Stderr = &errOut
	if code := quiet.agentTurnFrom(args, strings.NewReader(stdin)); code != 0 {
		return "agent-turn failed: " + strings.TrimSpace(errOut.String())
	}
	got, _, err := agent.ReadLastMessage(state.LastMessage("doctor", 1))
	if err != nil || got != want {
		return fmt.Sprintf("agent-turn wrote %q, want %q (%s)", got, want, strings.TrimSpace(errOut.String()))
	}
	out := a.DoctorEnv.Output
	if out == nil {
		out = func(ctx context.Context, path string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, path, args...).CombinedOutput()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	help, err := out(ctx, path, "--help")
	if err != nil {
		return cli + " --help: " + err.Error()
	}
	if !strings.Contains(string(help), flag+" <") {
		return fmt.Sprintf("%s --help does not list %s, which passes the turn hook; steps in a terminal would wait", cli, flag)
	}
	return ""
}
