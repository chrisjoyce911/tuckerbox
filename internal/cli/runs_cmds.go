package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/priority"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/scope"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// readyMarker starts the line a detached engine prints once it holds its
// change lock and has joined the queue; its parent then stops waiting.
const readyMarker = "TUCKERBOX-READY"

// stopWait is how long tuckerbox stop waits for a run to stop by itself
// before killing it: the agent's own stop grace (10s) plus slack.
const stopWait = 30 * time.Second

func (a App) executable() (string, error) {
	if a.Executable != nil {
		return a.Executable()
	}
	return os.Executable()
}

func (a App) stateHome() (string, error) {
	env, _ := a.configEnv()
	return runs.UserStateDir(env.Getenv, env.HomeDir, env.GOOS)
}

func (a App) errf(cmd, format string, args ...any) int {
	fmt.Fprintf(a.Stderr, "tuckerbox %s: %s\n", cmd, fmt.Sprintf(format, args...))
	return 1
}

// loadRepo opens the repository containing dir and loads its config,
// printing every problem.
func (a App) loadRepo(cmd, dir string) (runs.Repo, *config.Config, bool) {
	env, _ := a.configEnv()
	repo, err := runs.OpenRepo(context.Background(), dir)
	if err != nil {
		a.errf(cmd, "%v", err)
		return repo, nil, false
	}
	opts, err := config.Discover(env, repo.Root)
	if err != nil {
		a.errf(cmd, "%v", err)
		return repo, nil, false
	}
	cfg, err := config.Load(opts)
	var problems config.Problems
	switch {
	case errors.As(err, &problems):
		for _, p := range problems {
			p.File = displayPath(opts.RepoRoot, p.File)
			fmt.Fprintln(a.Stderr, p)
		}
		a.errf(cmd, "fix the config first (tuckerbox check)")
		return repo, nil, false
	case err != nil:
		a.errf(cmd, "%v", err)
		return repo, nil, false
	}
	return repo, cfg, true
}

// run: tuckerbox run [--foreground] [--tasks <scope>] <change>... | --all
func (a App) run(args []string) int {
	var all, fg bool
	var tasks string
	pos, err := parseArgs(args, map[string]*bool{"all": &all, "foreground": &fg}, map[string]*string{"tasks": &tasks})
	if err != nil {
		return a.usageErr("run", err.Error())
	}
	switch {
	case tasks != "" && (all || len(pos) != 1):
		return a.usageErr("run", "--tasks takes one change, not --all or several")
	case tasks != "":
		if err := scope.Valid(tasks); err != nil {
			return a.usageErr("run", err.Error())
		}
	case all && len(pos) > 0:
		return a.usageErr("run", "give change ids or --all, not both")
	case !all && len(pos) == 0:
		return a.usageErr("run", "give one or more change ids, or --all")
	case fg && (all || len(pos) != 1):
		return a.usageErr("run", "--foreground runs exactly one change")
	}
	_, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		return a.errf("run", "%v", err)
	}
	repo, cfg, ok := a.loadRepo("run", wd)
	if !ok {
		return 1
	}
	// Before anything is validated or made: agent_backend = "terminal"
	// without a working herdr-core refuses the run and leaves no state.
	if _, err := a.chooseBackend(cfg.AgentBackend); err != nil {
		return a.errf("run", "%v", err)
	}
	ctx := context.Background()
	ospec := runs.OpenSpec{Dir: repo.Root}
	changes := pos
	startRevs := map[string]string{} // the commit each change was checked at
	if all {
		ids, err := ospec.List(ctx)
		if err != nil {
			return a.errf("run", "%v", err)
		}
		changes = nil
		gate := &runGate{a: a, repo: repo, cfg: cfg}
		for _, id := range ids {
			if err := ospec.Validate(ctx, id); err != nil {
				fmt.Fprintf(a.Stderr, "tuckerbox run: skipping %s: %v\n", id, err)
				continue
			}
			rev, err := gate.check(ctx, id)
			if err == nil {
				err = gate.overrides(ctx, id)
			}
			if err != nil {
				var na *notApprovedError
				var oe *runs.OverridesError
				if errors.As(err, &na) {
					fmt.Fprintf(a.Stdout, "%s: skipped, not approved (%s)\n", id, na.State)
				} else if errors.As(err, &oe) {
					fmt.Fprintf(a.Stdout, "%s: skipped, step overrides not allowed\n", id)
				}
				fmt.Fprintf(a.Stderr, "tuckerbox run: skipping %s: %v\n", id, err)
				continue
			}
			changes = append(changes, id)
			startRevs[id] = rev
		}
		if len(changes) == 0 {
			fmt.Fprintln(a.Stdout, "no valid active changes to run")
			return 0
		}
		// Requested oldest first, started in admission order (each waits
		// for the one before to join the queue), so the queue agrees.
		all, err := priority.Load(repo.Checkout())
		if err != nil {
			return a.errf("run", "%v", err)
		}
		changes = priority.RunAll(changes, all)
	} else {
		bad := false
		gate := &runGate{a: a, repo: repo, cfg: cfg}
		for _, id := range changes {
			if err := statedir.CheckID("change", id); err != nil {
				a.errf("run", "%v", err)
				bad = true
				continue
			}
			if err := ospec.Validate(ctx, id); err != nil {
				a.errf("run", "%v", err)
				bad = true
				continue
			}
			rev, err := gate.check(ctx, id)
			if err == nil {
				err = gate.overrides(ctx, id)
			}
			if err != nil {
				a.errf("run", "%v", err)
				bad = true
				continue
			}
			startRevs[id] = rev
			if tasks != "" {
				if tasks, err = a.runScope(ctx, gate, id, tasks); err != nil {
					a.errf("run", "%v", err)
					bad = true
				}
			}
		}
		if bad {
			fmt.Fprintln(a.Stderr, "tuckerbox run: nothing started")
			return 1
		}
	}
	if !a.shippingPreflight(ctx, repo, cfg, changes) {
		return 1
	}
	if fg {
		return a.foreground(repo, cfg, runs.Request{Change: changes[0], Mode: runs.Start, StartRev: startRevs[changes[0]], Scope: tasks})
	}
	code := 0
	for _, id := range changes {
		engineArgs := []string{"--mode", "start", id}
		if rev := startRevs[id]; rev != "" {
			engineArgs = append([]string{"--start-rev", rev}, engineArgs...)
		}
		if tasks != "" {
			engineArgs = append([]string{"--tasks", tasks}, engineArgs...)
		}
		err := a.spawn(repo.Root, engineArgs)
		var se *runs.StateError
		var ae *runs.ActiveError
		var ee *runs.EditingError
		switch {
		case err == nil:
		case all && errors.As(err, &se):
			fmt.Fprintf(a.Stdout, "%s: skipped, %v\n", id, err)
		case all && errors.As(err, &ae):
			fmt.Fprintf(a.Stdout, "%s: skipped, already running\n", id)
		case all && errors.As(err, &ee):
			fmt.Fprintf(a.Stdout, "%s: skipped, being edited\n", id)
		default:
			fmt.Fprintf(a.Stderr, "tuckerbox run: %s: %v\n", id, err)
			code = 1
		}
	}
	return code
}

func (a App) usageErr(cmd, msg string) int {
	fmt.Fprintf(a.Stderr, "tuckerbox %s: %s\n\n%s", cmd, msg, usage)
	return 2
}

// spawnError carries a detached engine's own message, so run can tell a
// change that is already running or ready from other failures.
type spawnError struct {
	msg string
	err error
}

func (e *spawnError) Error() string { return e.msg }
func (e *spawnError) Unwrap() error { return e.err }

// spawn starts a detached engine process for one run in dir and waits until
// it holds its change lock and has joined the queue, or has failed.
func (a App) spawn(dir string, engineArgs []string) error {
	exe, err := a.executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, append([]string{"engine", "--handshake"}, engineArgs...)...)
	cmd.Dir = dir
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	detach(cmd)
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return err
	}
	pw.Close()
	defer pr.Close()
	sc := bufio.NewScanner(pr)
	var lines []string
	for sc.Scan() {
		line := sc.Text()
		if rest, ok := strings.CutPrefix(line, readyMarker+" "); ok {
			for _, l := range lines {
				fmt.Fprintln(a.Stdout, l)
			}
			fmt.Fprintln(a.Stdout, rest)
			return cmd.Process.Release()
		}
		lines = append(lines, line)
	}
	_ = cmd.Wait()
	msg := strings.TrimSpace(strings.Join(lines, "\n"))
	msg = strings.TrimPrefix(msg, "tuckerbox engine: ")
	if msg == "" {
		msg = fmt.Sprintf("the run process exited (%v) before it started", cmd.ProcessState)
	}
	se := &spawnError{msg: msg}
	switch {
	case strings.Contains(msg, "already has an active run"):
		se.err = &runs.ActiveError{}
	case strings.Contains(msg, " is being edited "):
		se.err = &runs.EditingError{}
	case strings.HasPrefix(msg, "change ") && strings.Contains(msg, " is "+string(runs.Ready)):
		se.err = &runs.StateError{State: runs.Ready}
	}
	return se
}

// switchWriter is a writer whose target changes once the run's own log
// file is known.
type switchWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *switchWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w == nil {
		return len(p), nil
	}
	return s.w.Write(p)
}

func (s *switchWriter) set(w io.Writer) {
	s.mu.Lock()
	s.w = w
	s.mu.Unlock()
}

// engine runs one run in this process. Users reach it through run, resume
// and rerun; --handshake is for the detached process they start.
//
//	tuckerbox engine [--handshake] --mode start|resume|rerun [--step s] [--worktree p] [--start-rev c] [--tasks scope] <change>
func (a App) engine(args []string) int {
	var handshake, expectNone bool
	var mode, step, worktree, startRev, tasks, expectState, expectSince, by string
	pos, err := parseArgs(args, map[string]*bool{"handshake": &handshake, "expect-none": &expectNone},
		map[string]*string{"mode": &mode, "step": &step, "worktree": &worktree, "start-rev": &startRev, "tasks": &tasks,
			"expect-state": &expectState, "expect-since": &expectSince, "by": &by})
	if err != nil || len(pos) != 1 {
		return a.usageErr("engine", "usage: tuckerbox engine --mode start|resume|rerun [--step s] [--worktree p] <change>")
	}
	req := runs.Request{Change: pos[0], Step: step, Worktree: worktree, StartRev: startRev, Scope: tasks, By: by}
	switch {
	case expectNone:
		req.Expect = &runs.Expect{NoRun: true}
	case expectState != "":
		since, err := time.Parse(time.RFC3339Nano, expectSince)
		if err != nil {
			return a.usageErr("engine", "--expect-state needs --expect-since as an RFC 3339 time")
		}
		req.Expect = &runs.Expect{State: runs.State(expectState), Since: since}
	}
	switch mode {
	case "start":
		req.Mode = runs.Start
	case "resume":
		req.Mode = runs.Resume
	case "rerun":
		req.Mode = runs.Rerun
		if step == "" {
			return a.usageErr("engine", "rerun needs --step")
		}
	default:
		return a.usageErr("engine", "unknown --mode "+mode)
	}
	_, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		return a.errf("engine", "%v", err)
	}
	repo, cfg, ok := a.loadRepo("engine", wd)
	if !ok {
		return 1
	}
	if !handshake {
		return a.foreground(repo, cfg, req)
	}
	logw := &switchWriter{w: a.Stderr}
	var logFile *os.File
	e, err := a.newEngine(repo, cfg, logw)
	if err != nil {
		return a.errf("engine", "%v", err)
	}
	e.OnLocked = func(ws runs.Workspace, r *runs.Run) {
		f, err := os.OpenFile(runs.EngineLog(ws.State), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			logFile = f
		}
		fmt.Fprintf(a.Stdout, "%s %s: started (pid %d, %s) in %s\n", readyMarker, r.Change, os.Getpid(), r.State, ws.Path)
		afterHandshake()
		// The parent has stopped reading: from now on, only the run's log.
		if logFile != nil {
			logw.set(logFile)
		} else {
			logw.set(nil)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), stopSignals...)
	defer stop()
	r, err := e.Run(ctx, req)
	if logFile != nil {
		defer logFile.Close()
	}
	if err != nil {
		fmt.Fprintf(logw, "tuckerbox engine: %v\n", err)
		return 1
	}
	return exitFor(r)
}

// newEngine makes the engine for a run in repo. It fails when the
// machine's agent backend can't be used (agent_backend = "terminal"
// without herdr-core), before the run changes anything.
func (a App) newEngine(repo runs.Repo, cfg *config.Config, log io.Writer) (*runs.Engine, error) {
	backend, err := a.agentBackend(cfg.AgentBackend)
	if err != nil {
		return nil, err
	}
	e := &runs.Engine{Repo: repo, Config: cfg, Log: log, Backend: backend}
	if a.Forge != nil {
		e.Forge = func(ctx context.Context) (forge.Client, func(string) string, error) { return a.Forge(ctx, cfg) }
	}
	if cfg.RequireApproval {
		e.Gate = approvalGate(repo)
		e.SettingsGate = func(ctx context.Context, change, worktree string) error {
			return settingsGate(ctx, repo.Root, change, worktree)
		}
		e.ResumeGate = func(ctx context.Context, change, worktree string) error {
			// The approvals ref wins over the sidecar, as in the run gate.
			entry, err := refEntry(ctx, repo.Root, change)
			if err != nil {
				return err
			}
			st, err := approval.JudgeWith(approval.DirOf(worktree, change), entry)
			if err != nil {
				return err
			}
			if st.State == approval.None {
				return fmt.Errorf("change %s is no longer approved", change)
			}
			return nil
		}
	}
	if home, err := a.stateHome(); err == nil {
		e.StateHome = home
	} else {
		fmt.Fprintf(log, "warning: no machine index: %v\n", err)
	}
	return e, nil
}

// foreground runs req in this process, attached to the terminal; Ctrl-C
// stops it.
func (a App) foreground(repo runs.Repo, cfg *config.Config, req runs.Request) int {
	logw := &switchWriter{w: a.Stdout}
	e, err := a.newEngine(repo, cfg, logw)
	if err != nil {
		return a.errf("run", "%v", err)
	}
	var logFile *os.File
	e.OnLocked = func(ws runs.Workspace, r *runs.Run) {
		if f, err := os.OpenFile(runs.EngineLog(ws.State), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			logFile = f
			logw.set(io.MultiWriter(a.Stdout, f))
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), stopSignals...)
	defer stop()
	r, err := e.Run(ctx, req)
	if logFile != nil {
		defer logFile.Close()
	}
	if err != nil {
		return a.errf("run", "%v", err)
	}
	return exitFor(r)
}

// exitFor is 0 when the run is ready or paused for a person (blocked or
// stopped), 1 when it failed.
func exitFor(r *runs.Run) int {
	if r.State == runs.Failed {
		return 1
	}
	return 0
}

// located is a change's run found on this machine.
type located struct {
	State statedir.Dir
	Run   *runs.Run
}

func (l located) active() bool { return runs.Active(l.State.RunLock()) }

// locate finds change's run: in the repository around the working
// directory first, otherwise in the machine index.
func (a App) locate(change string) (located, error) {
	ctx := context.Background()
	if err := statedir.CheckID("change", change); err != nil {
		return located{}, err
	}
	_, getwd := a.configEnv()
	if wd, err := getwd(); err == nil {
		if repo, err := runs.OpenRepo(ctx, wd); err == nil {
			if wts, err := repo.Worktrees(ctx); err == nil {
				for _, wt := range wts {
					st, err := statedir.Resolve(ctx, wt)
					if err != nil {
						continue
					}
					if r, err := runs.ReadRun(st.RunFile()); err == nil && r.Change == change {
						return located{State: st, Run: r}, nil
					}
				}
			}
		}
	}
	home, err := a.stateHome()
	if err != nil {
		return located{}, err
	}
	ix, err := runs.ReadIndex(home)
	if err != nil {
		return located{}, err
	}
	var found []located
	for _, e := range ix.Runs {
		if e.Change != change {
			continue
		}
		st := statedir.Dir(e.State)
		if r, err := runs.ReadRun(st.RunFile()); err == nil && r.Change == change {
			found = append(found, located{State: st, Run: r})
		}
	}
	switch len(found) {
	case 0:
		return located{}, &noRunError{change}
	case 1:
		return found[0], nil
	}
	var repos []string
	for _, l := range found {
		repos = append(repos, l.Run.Repo)
	}
	return located{}, fmt.Errorf("change %s has runs in several repositories (%s); run the command inside one of them",
		change, strings.Join(repos, ", "))
}

// stop: tuckerbox stop <change>
func (a App) stop(args []string) int {
	pos, err := parseArgs(args, nil, nil)
	if err != nil || len(pos) != 1 {
		return a.usageErr("stop", "usage: tuckerbox stop <change>")
	}
	l, err := a.locate(pos[0])
	if err != nil {
		return a.errf("stop", "%v", err)
	}
	if !l.active() {
		// The run process died; its agent may work on in a terminal.
		if code, ok := a.stopOrphan(pos[0], l.State); ok {
			return code
		}
		v := l.Run.Effective(false)
		return a.errf("stop", "change %s has no active run (it is %s)", pos[0], v.State)
	}
	pid := runs.ReadPid(l.State)
	if err := os.WriteFile(runs.StopFile(l.State), nil, 0o644); err != nil {
		return a.errf("stop", "%v", err)
	}
	if pid > 0 {
		_ = signalStop(pid)
	}
	deadline := time.Now().Add(stopWait)
	for l.active() {
		if time.Now().After(deadline) {
			agentPid := 0
			if r, err := runs.ReadRun(l.State.RunFile()); err == nil {
				agentPid = r.AgentPid
			}
			killRun(pid, agentPid)
			fmt.Fprintf(a.Stdout, "%s: did not stop within %s; killed the run process (pid %d)\n", pos[0], stopWait, pid)
			// Its agent may outlive it in a terminal: end that too.
			for i := 0; i < 50 && l.active(); i++ {
				time.Sleep(100 * time.Millisecond)
			}
			if code, ok := a.stopOrphan(pos[0], l.State); ok {
				return code
			}
			return 0
		}
		time.Sleep(100 * time.Millisecond)
	}
	r, err := runs.ReadRun(l.State.RunFile())
	if err != nil {
		return a.errf("stop", "%v", err)
	}
	if r.Orphaned(false) {
		// The run process could not end its agent's terminal: it left the
		// run running with the terminal recorded. Try again with the lock.
		fmt.Fprintf(a.Stderr, "tuckerbox stop: %s: the run process did not end its agent's terminal (%s); trying again\n", r.Change, oneLine(r.Reason))
		if code, ok := a.stopOrphan(pos[0], l.State); ok {
			return code
		}
	}
	v := r.Effective(false)
	fmt.Fprintf(a.Stdout, "%s: %s at step %s (resume with tuckerbox resume %s)\n", v.Change, v.State, v.Step, v.Change)
	return 0
}

// stopOrphan stops a run whose process died while its agent works on in a
// terminal: it takes the change lock, ends the terminal by name, records
// the run stopped and lets the lock go (formal/terminals.tla, Stop with
// the lock free). ok is false when the run has no agent terminal.
func (a App) stopOrphan(change string, st statedir.Dir) (code int, ok bool) {
	hc, found := a.hc()
	end := func(ctx context.Context, name string) error {
		if !found {
			return errors.New("herdr-core's hc is not on the PATH, so the agent's terminal can't be ended")
		}
		return hc.End(ctx, name, 30*time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	save := func(logPath, step string, round int, name string) error {
		if !found {
			return nil
		}
		err := agent.Terminal{HC: hc}.SaveHistory(logPath, step, round, name)
		if err != nil {
			fmt.Fprintf(a.Stderr, "tuckerbox stop: %s: saving the agent's session to the %s log: %v\n", change, step, err)
		}
		return err
	}
	r, err := runs.StopOrphan(ctx, st, end, save)
	switch {
	case errors.Is(err, runs.ErrNoOrphan), errors.Is(err, runs.ErrNoRun):
		return 0, false
	case errors.Is(err, runs.ErrLocked):
		return a.errf("stop", "change %s was started again meanwhile; run tuckerbox stop %s again", change, change), true
	case err != nil:
		return a.errf("stop", "%v", err), true
	}
	fmt.Fprintf(a.Stdout, "%s: ended the agent's terminal; %s at step %s (resume with tuckerbox resume %s)\n",
		change, r.State, r.Step, change)
	return 0, true
}

// resume: tuckerbox resume [--foreground] <change>
func (a App) resume(args []string) int {
	var fg bool
	pos, err := parseArgs(args, map[string]*bool{"foreground": &fg}, nil)
	if err != nil || len(pos) != 1 {
		return a.usageErr("resume", "usage: tuckerbox resume [--foreground] <change>")
	}
	return a.restart("resume", fg, pos[0], runs.Request{Change: pos[0], Mode: runs.Resume})
}

// rerun: tuckerbox rerun [--foreground] <change> <step>
func (a App) rerun(args []string) int {
	var fg bool
	pos, err := parseArgs(args, map[string]*bool{"foreground": &fg}, nil)
	if err != nil || len(pos) != 2 {
		return a.usageErr("rerun", "usage: tuckerbox rerun [--foreground] <change> <step>")
	}
	if err := statedir.CheckID("step", pos[1]); err != nil {
		return a.errf("rerun", "%v", err)
	}
	return a.restart("rerun", fg, pos[0], runs.Request{Change: pos[0], Mode: runs.Rerun, Step: pos[1]})
}

func (a App) restart(cmd string, fg bool, change string, req runs.Request) int {
	l, err := a.locate(change)
	if err != nil {
		return a.errf(cmd, "%v", err)
	}
	if l.active() {
		return a.errf(cmd, "%v", runs.LockedError(change, l.Run.Worktree, l.State))
	}
	req.Worktree = l.Run.Worktree
	repo, cfg, ok := a.loadRepo(cmd, l.Run.Repo)
	if !ok {
		return 1
	}
	// A clear refusal before any process starts; the engine checks again
	// under the change lock.
	if err := runs.CheckOverrides(context.Background(), cfg, repo.Root, change, runs.StartPoint{Worktree: req.Worktree}); err != nil {
		return a.errf(cmd, "%v", err)
	}
	if cfg.RequireApproval {
		if err := settingsGate(context.Background(), repo.Root, change, req.Worktree); err != nil {
			return a.errf(cmd, "%v", err)
		}
	}
	if fg {
		return a.foreground(repo, cfg, req)
	}
	args := []string{"--mode", cmd, "--worktree", req.Worktree}
	if req.Step != "" {
		args = append(args, "--step", req.Step)
	}
	if err := a.spawn(l.Run.Repo, append(args, change)); err != nil {
		return a.errf(cmd, "%v", err)
	}
	return 0
}

// logs: tuckerbox logs <change> [step] [--follow]
func (a App) logs(args []string) int {
	var follow bool
	pos, err := parseArgs(args, map[string]*bool{"follow": &follow, "f": &follow}, nil)
	if err != nil || len(pos) < 1 || len(pos) > 2 {
		return a.usageErr("logs", "usage: tuckerbox logs <change> [step] [--follow]")
	}
	l, err := a.locate(pos[0])
	if err != nil {
		return a.errf("logs", "%v", err)
	}
	step, named := "", len(pos) == 2
	if named {
		step = pos[1]
	} else {
		step = l.Run.Step
		if step == "" && len(l.Run.History) > 0 {
			step = l.Run.History[len(l.Run.History)-1].Step
		}
		if step == "" {
			return a.errf("logs", "change %s has run no step yet", pos[0])
		}
	}
	if err := statedir.CheckID("step", step); err != nil {
		return a.errf("logs", "%v", err)
	}
	// A step whose agent works in a live terminal: read the terminal.
	if hc, pin, ok := a.liveTerminal(l.Run, step); ok {
		return a.terminalLogs(hc, pin, l.Run.Terminal.Name, l, follow)
	}
	if !follow {
		if err := agent.ReadLog(l.State.Log(step), a.Stdout); err != nil {
			if errors.Is(err, agent.ErrNoLog) {
				return a.errf("logs", "step %s of %s has no log yet", step, pos[0])
			}
			return a.errf("logs", "%v", err)
		}
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), stopSignals...)
	defer stop()
	// read returns the run as people see it, and whether it is queued or
	// running.
	read := func() (*runs.Run, bool) {
		r, err := runs.ReadRun(l.State.RunFile())
		if err != nil {
			return l.Run, false
		}
		v := r.Effective(l.active())
		return &v, v.State == runs.Running || v.State == runs.Queued
	}
	seen := len(l.Run.History)
	for {
		s := step
		err := agent.FollowLog(ctx, l.State.Log(s), a.Stdout, agent.FollowOptions{
			WaitForFile: true,
			Done: func() bool {
				r, live := read()
				return !live || r.Step != s
			},
		})
		if err != nil && !errors.Is(err, agent.ErrNoLog) && ctx.Err() == nil {
			return a.errf("logs", "%v", err)
		}
		if named || ctx.Err() != nil {
			return 0
		}
		// Without a named step, follow the run on: first the steps that
		// started and finished since the last look, then the running one.
		r, live := read()
		var done []string
		if seen <= len(r.History) {
			for i, e := range r.History[seen:] {
				if (i == 0 && e.Step == s) || (len(done) > 0 && done[len(done)-1] == e.Step) {
					continue
				}
				done = append(done, e.Step)
			}
		}
		seen = len(r.History)
		for _, d := range done {
			if live && d == r.Step {
				continue
			}
			fmt.Fprintf(a.Stdout, "\n==> tuckerbox: step %s <==\n", d)
			_ = agent.ReadLog(l.State.Log(d), a.Stdout)
		}
		if !live {
			return 0
		}
		fmt.Fprintf(a.Stdout, "\n==> tuckerbox: step %s <==\n", r.Step)
		step = r.Step
	}
}

// liveTerminal returns the terminal of r's agent when step is r's current
// step and its terminal lives, as it was then: its address and labels pin
// it, so logs can tell when the next step's terminal replaces it.
func (a App) liveTerminal(r *runs.Run, step string) (agent.HC, *agent.HCTerminal, bool) {
	if r == nil || r.Terminal == nil || r.State != runs.Running || r.Step != step {
		return agent.HC{}, nil, false
	}
	hc, ok := a.hc()
	if !ok {
		return agent.HC{}, nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	t, err := hc.Get(ctx, r.Terminal.Name)
	if err != nil || !t.Live() || !agent.MatchesStep(t, r.Change, r.Step, r.Round) {
		return agent.HC{}, nil, false
	}
	if t.Name == "" {
		t.Name = r.Terminal.Name
	}
	return hc, t, true
}

// termSource is what logs needs of herdr-core to show a terminal.
type termSource interface {
	Get(ctx context.Context, name string) (*agent.HCTerminal, error)
	Read(ctx context.Context, name string) ([]byte, error)
}

// terminalLogs shows a live agent terminal's history through hc read and,
// with follow, what it writes next until it ends. Its history goes into
// the step's log file when the step ends, where logs reads it after.
func (a App) terminalLogs(hc agent.HC, pin *agent.HCTerminal, name string, l located, follow bool) int {
	ctx, stop := signal.NotifyContext(context.Background(), stopSignals...)
	defer stop()
	step := l.Run.Step
	err := followTerminal(ctx, hc, name, pin, l.Run.Change, step, l.Run.Round, a.Stdout, follow, 500*time.Millisecond,
		func() bool {
			r, _ := runs.ReadRun(l.State.RunFile())
			return r != nil && r.Terminal != nil && r.Terminal.Name == name && r.Step == step && r.Round == l.Run.Round
		})
	if err != nil {
		return a.errf("logs", "%v", err)
	}
	if follow && ctx.Err() == nil {
		fmt.Fprintf(a.Stdout, "\n==> tuckerbox: the agent's terminal %s ended; tuckerbox logs %s %s shows its session <==\n",
			name, l.Run.Change, step)
	}
	return 0
}

// followTerminal writes the history of the terminal called name, pinned
// to pin's address and to change, step and round, to w; with follow, it
// polls every interval and writes what is new until the terminal ends,
// is replaced by another (the next step's terminal under the same name),
// or still says the run has let it go. Output is written only when the
// pinned terminal is there both before and after it is read, so another
// step's terminal is never shown as this one's.
func followTerminal(ctx context.Context, src termSource, name string, pin *agent.HCTerminal, change, step string, round int,
	w io.Writer, follow bool, interval time.Duration, still func() bool) error {
	same := func(t *agent.HCTerminal) bool {
		return t != nil && t.Address == pin.Address && agent.MatchesStep(t, change, step, round)
	}
	// read reads the terminal and reports whether it is still the pinned
	// one afterwards (ok), and whether it lives. rerr is hc read's error:
	// there is nothing to show this time.
	read := func() (out []byte, rerr error, ok, live bool, err error) {
		out, rerr = src.Read(ctx, name)
		t, err := src.Get(ctx, name)
		if err != nil {
			return nil, nil, false, false, err
		}
		if !same(t) {
			return nil, nil, false, false, nil
		}
		return out, rerr, true, t.Live(), nil
	}
	quiet := func(err error) error {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	out, rerr, ok, live, err := read()
	switch {
	case err != nil:
		return quiet(err)
	case !ok:
		return nil
	case rerr != nil:
		return quiet(rerr)
	}
	_, _ = w.Write(out)
	if !follow {
		return nil
	}
	prev := out
	for live {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
		t, err := src.Get(ctx, name)
		if err != nil {
			return quiet(err)
		}
		if !same(t) {
			return nil
		}
		cur, rerr, ok, l, err := read()
		if err != nil {
			return quiet(err)
		}
		if !ok {
			return nil
		}
		live = l
		if rerr != nil {
			continue
		}
		// Only what is new since the last read.
		n := 0
		for n < len(prev) && n < len(cur) && prev[n] == cur[n] {
			n++
		}
		_, _ = w.Write(cur[n:])
		prev = cur
		if live && still != nil && !still() {
			return nil
		}
	}
	return nil
}

// displayTime is how status shows a time: the machine's local time zone.
func displayTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04 MST")
}

// statusRun is one run in tuckerbox status --json. Fields may be added
// within a version, never renamed or removed.
type statusRun struct {
	MR        *ship.MRInfo `json:"mr,omitempty"`
	CI        *ship.CIInfo `json:"ci,omitempty"`
	ShipRound int          `json:"ship_round,omitempty"`
	Repo      string       `json:"repo"`
	Change    string       `json:"change"`
	State     string       `json:"state"`
	Step      string       `json:"step"`
	Round     int          `json:"round"`
	Since     string       `json:"since"`
	Reason    string       `json:"reason"`
	Worktree  string       `json:"worktree"`
	Branch    string       `json:"branch"`
	Pid       int          `json:"pid,omitempty"`
	Created   string       `json:"created"`
	// AgentBackend is where the current or last step's agent runs:
	// "terminal" or "headless".
	AgentBackend string `json:"agent_backend,omitempty"`
	// Terminal is the agent's herdr-core terminal address while a step
	// runs in one; AgentState is "working" or "waiting for you" there.
	Terminal   string `json:"terminal,omitempty"`
	AgentState string `json:"agent_state,omitempty"`
	// NotWatched is set on a running run whose process died while its
	// agent works on in a terminal; resume adopts it.
	NotWatched bool `json:"not_watched,omitempty"`
	// Scope is the run's task scope; absent for the whole change.
	Scope *scope.Scope `json:"scope,omitempty"`
	// Question is what the run asks the person: a blocked result's
	// summary and findings, with the step and round that asked
	// (blocked-answers), or a waiting terminal agent's last message.
	Question *runs.QuestionView `json:"question,omitempty"`
}

// StatusVersion is the format version of tuckerbox status --json.
const StatusVersion = 1

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format(time.RFC3339)
}

// statusRuns returns every known run on the machine, as people should see
// it, sorted by repository and change.
func (a App) statusRuns() ([]statusRun, error) {
	home, err := a.stateHome()
	if err != nil {
		return nil, err
	}
	ix, err := runs.ReadIndex(home)
	if err != nil {
		return nil, err
	}
	out := []statusRun{}
	for _, e := range ix.Runs {
		st := statedir.Dir(e.State)
		r, err := runs.ReadRun(st.RunFile())
		if err != nil {
			continue // worktree removed, or not started
		}
		v, watched := runs.Seen(context.Background(), r, runs.Active(st.RunLock()))
		sr := statusRun{
			Repo: e.Repo, Change: v.Change, State: string(v.State), Step: v.Step, Round: v.Round,
			Since: rfc3339(v.Since), Reason: v.Reason, Worktree: v.Worktree, Branch: v.Branch,
			Pid: v.Pid, Created: rfc3339(v.Created), Scope: v.Scope, MR: v.MR, CI: v.CI, ShipRound: v.ShipRound,
			AgentBackend: v.AgentBackend, AgentState: v.AgentState, NotWatched: !watched,
		}
		if v.State == runs.Running && v.Terminal != nil {
			sr.Terminal = v.Terminal.Address
		}
		sr.Question = runs.Ask(&v, st, v.AgentState)
		out = append(out, sr)
	}
	return out, nil
}

// status: tuckerbox status [--json]
func (a App) status(args []string) int {
	var asJSON bool
	pos, err := parseArgs(args, map[string]*bool{"json": &asJSON}, nil)
	if err != nil || len(pos) > 0 {
		return a.usageErr("status", "usage: tuckerbox status [--json]")
	}
	list, err := a.statusRuns()
	if err != nil {
		return a.errf("status", "%v", err)
	}
	if asJSON {
		return a.writeJSON(map[string]any{"version": StatusVersion, "runs": list})
	}
	if len(list) == 0 {
		fmt.Fprintln(a.Stdout, "no runs on this machine")
		return 0
	}
	rows := [][]string{{"REPO", "CHANGE", "STATE", "STEP", "TASKS", "ROUND", "SINCE", "MR", "CI", "SHIP ROUND", "REASON"}}
	for _, r := range list {
		since, _ := time.Parse(time.RFC3339, r.Since)
		step := r.Step
		if step == "" {
			step = "-"
		}
		tasks := "all"
		if r.Scope != nil {
			tasks = r.Scope.Spec
		}
		mr, ci, shipRound := "", "", ""
		if r.MR != nil {
			mr = r.MR.Link
		}
		if r.CI != nil {
			ci = r.CI.State
		}
		if r.ShipRound > 0 {
			shipRound = strconv.Itoa(r.ShipRound)
		}
		state := r.State
		if r.NotWatched {
			state += " (not watched)"
		}
		rows = append(rows, []string{filepath.Base(r.Repo), r.Change, state, step, tasks, strconv.Itoa(r.Round), displayTime(since), mr, ci, shipRound, r.Reason})
	}
	writeTable(a.Stdout, rows)
	for _, r := range list {
		if q := r.Question; q != nil {
			writeQuestion(a.Stdout, r.Change, q)
		}
	}
	return 0
}

// writeQuestion prints a run's question under the status table.
func writeQuestion(w io.Writer, change string, q *runs.QuestionView) {
	verb := "asks"
	if q.Kind == runs.QuestionWaiting {
		verb = "waits for you"
	}
	fmt.Fprintf(w, "\n%s %s (step %s, round %d):\n", change, verb, q.Step, q.Round)
	if q.Summary != "" {
		for _, line := range strings.Split(q.Summary, "\n") {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
	for _, f := range q.Findings {
		where := f.File
		if where == "" {
			where = "(whole change)"
		}
		fmt.Fprintf(w, "  - [%s] %s: %s\n", f.Severity, where, oneLine(f.Detail))
	}
	if n := len(q.Answers); n > 0 {
		fmt.Fprintf(w, "  answered %d time(s); tuckerbox resume %s continues with the answers\n", n, change)
	} else {
		fmt.Fprintf(w, "  answer with tuckerbox answer %s <text>\n", change)
	}
}

func (a App) writeJSON(v any) int {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return a.errf("status", "%v", err)
	}
	fmt.Fprintln(a.Stdout, string(data))
	return 0
}

func writeTable(w io.Writer, rows [][]string) {
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, c := range row {
			if n := len([]rune(c)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	for _, row := range rows {
		var b strings.Builder
		for i, c := range row {
			if i == len(row)-1 {
				b.WriteString(c)
				break
			}
			b.WriteString(c)
			b.WriteString(strings.Repeat(" ", widths[i]-len([]rune(c))+2))
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
}

// runScope judges the task scope spec of tuckerbox run --tasks in the
// tasks.md the run will use (its worktree, else the commit a new worktree
// would be made at), before any worktree or run is made, and returns it
// with next resolved to a group number.
func (a App) runScope(ctx context.Context, g *runGate, change, spec string) (string, error) {
	sp, err := g.start(ctx, change)
	if err != nil {
		return "", err
	}
	var data []byte
	var cur *runs.Run
	if sp.Worktree != "" {
		data, err = os.ReadFile(runs.TasksFile(sp.Worktree, change))
		if st, serr := statedir.Resolve(ctx, sp.Worktree); serr == nil {
			if r, rerr := runs.ReadRun(st.RunFile()); rerr == nil {
				cur = r
			}
		}
	} else {
		var rev *approval.Rev
		if rev, err = approval.NewRev(ctx, g.repo.Root, sp.Rev, change); err == nil {
			data, err = rev.Read("tasks.md")
		}
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("change %s: reading tasks.md: %v", change, err)
	}
	sc, same, err := runs.ScopeFor(spec, string(data), cur)
	if err != nil {
		return "", fmt.Errorf("change %s: %v", change, err)
	}
	if same {
		return cur.Scope.Spec, nil
	}
	return sc.Spec, nil
}
