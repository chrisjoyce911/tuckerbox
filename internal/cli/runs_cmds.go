package cli

import (
	"bufio"
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
	"sync"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
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

// run: tuckerbox run [--foreground] <change>... | --all
func (a App) run(args []string) int {
	var all, fg bool
	pos, err := parseArgs(args, map[string]*bool{"all": &all, "foreground": &fg}, nil)
	if err != nil {
		return a.usageErr("run", err.Error())
	}
	switch {
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
	ctx := context.Background()
	ospec := runs.OpenSpec{Dir: repo.Root}
	changes := pos
	if all {
		ids, err := ospec.List(ctx)
		if err != nil {
			return a.errf("run", "%v", err)
		}
		changes = nil
		for _, id := range ids {
			if err := ospec.Validate(ctx, id); err != nil {
				fmt.Fprintf(a.Stderr, "tuckerbox run: skipping %s: %v\n", id, err)
				continue
			}
			changes = append(changes, id)
		}
		if len(changes) == 0 {
			fmt.Fprintln(a.Stdout, "no valid active changes to run")
			return 0
		}
	} else {
		bad := false
		for _, id := range changes {
			if err := statedir.CheckID("change", id); err != nil {
				a.errf("run", "%v", err)
				bad = true
				continue
			}
			if err := ospec.Validate(ctx, id); err != nil {
				a.errf("run", "%v", err)
				bad = true
			}
		}
		if bad {
			fmt.Fprintln(a.Stderr, "tuckerbox run: nothing started")
			return 1
		}
	}
	if fg {
		return a.foreground(repo, cfg, runs.Request{Change: changes[0], Mode: runs.Start})
	}
	code := 0
	for _, id := range changes {
		err := a.spawn(repo.Root, []string{"--mode", "start", id})
		var se *runs.StateError
		var ae *runs.ActiveError
		switch {
		case err == nil:
		case all && errors.As(err, &se):
			fmt.Fprintf(a.Stdout, "%s: skipped, %v\n", id, err)
		case all && errors.As(err, &ae):
			fmt.Fprintf(a.Stdout, "%s: skipped, already running\n", id)
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
//	tuckerbox engine [--handshake] --mode start|resume|rerun [--step s] [--worktree p] <change>
func (a App) engine(args []string) int {
	var handshake bool
	var mode, step, worktree string
	pos, err := parseArgs(args, map[string]*bool{"handshake": &handshake},
		map[string]*string{"mode": &mode, "step": &step, "worktree": &worktree})
	if err != nil || len(pos) != 1 {
		return a.usageErr("engine", "usage: tuckerbox engine --mode start|resume|rerun [--step s] [--worktree p] <change>")
	}
	req := runs.Request{Change: pos[0], Step: step, Worktree: worktree}
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
	e := a.newEngine(repo, cfg, logw)
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

func (a App) newEngine(repo runs.Repo, cfg *config.Config, log io.Writer) *runs.Engine {
	e := &runs.Engine{Repo: repo, Config: cfg, Log: log}
	if home, err := a.stateHome(); err == nil {
		e.StateHome = home
	} else {
		fmt.Fprintf(log, "warning: no machine index: %v\n", err)
	}
	return e
}

// foreground runs req in this process, attached to the terminal; Ctrl-C
// stops it.
func (a App) foreground(repo runs.Repo, cfg *config.Config, req runs.Request) int {
	logw := &switchWriter{w: a.Stdout}
	e := a.newEngine(repo, cfg, logw)
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
			return 0
		}
		time.Sleep(100 * time.Millisecond)
	}
	r, err := runs.ReadRun(l.State.RunFile())
	if err != nil {
		return a.errf("stop", "%v", err)
	}
	v := r.Effective(false)
	fmt.Fprintf(a.Stdout, "%s: %s at step %s (resume with tuckerbox resume %s)\n", v.Change, v.State, v.Step, v.Change)
	return 0
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
		return a.errf(cmd, "%v", &runs.ActiveError{Change: change, Worktree: l.Run.Worktree, Pid: runs.ReadPid(l.State)})
	}
	req.Worktree = l.Run.Worktree
	if fg {
		repo, cfg, ok := a.loadRepo(cmd, l.Run.Repo)
		if !ok {
			return 1
		}
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
	Repo     string `json:"repo"`
	Change   string `json:"change"`
	State    string `json:"state"`
	Step     string `json:"step"`
	Round    int    `json:"round"`
	Since    string `json:"since"`
	Reason   string `json:"reason"`
	Worktree string `json:"worktree"`
	Branch   string `json:"branch"`
	Pid      int    `json:"pid,omitempty"`
	Created  string `json:"created"`
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
		v := r.Effective(runs.Active(st.RunLock()))
		out = append(out, statusRun{
			Repo: e.Repo, Change: v.Change, State: string(v.State), Step: v.Step, Round: v.Round,
			Since: rfc3339(v.Since), Reason: v.Reason, Worktree: v.Worktree, Branch: v.Branch,
			Pid: v.Pid, Created: rfc3339(v.Created),
		})
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
	rows := [][]string{{"REPO", "CHANGE", "STATE", "STEP", "ROUND", "SINCE", "REASON"}}
	for _, r := range list {
		since, _ := time.Parse(time.RFC3339, r.Since)
		step := r.Step
		if step == "" {
			step = "-"
		}
		rows = append(rows, []string{filepath.Base(r.Repo), r.Change, r.State, step, strconv.Itoa(r.Round), displayTime(since), r.Reason})
	}
	writeTable(a.Stdout, rows)
	return 0
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
