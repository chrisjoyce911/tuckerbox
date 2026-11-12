package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// The terminal tests run real tuckerbox processes against the fake hc,
// whose terminals outlive the run processes that start them, as
// herdr-core's do. The fake agent runs interactively in them.

// newTermCLI is newCLI with the terminal backend: the fake hc's server is
// up (agent_backend "auto") and the fake agent has an interactive
// command. By default each agent exits after its last turn.
func newTermCLI(t *testing.T, o testenv.RepoOptions) *cliFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake hc has no terminals on Windows")
	}
	o.Interactive = true
	f := newCLI(t, o)
	t.Setenv("FAKEHC_SERVER", "up")
	f.plan(map[string]testenv.Action{})
	return f
}

// plan sets the fake agent's plan; "*" exits after its last turn unless
// the plan says otherwise.
func (f *cliFixture) plan(p map[string]testenv.Action) {
	if _, ok := p["*"]; !ok {
		p["*"] = testenv.Action{ExitAfter: true}
	}
	f.env.SetPlan(f.t, p)
}

func (f *cliFixture) hc() agent.HC { return agent.HC{Path: f.env.HC} }

// terminalOf returns id's agent terminal, or nil.
func (f *cliFixture) terminalOf(id string) *agent.HCTerminal {
	f.t.Helper()
	r := f.runOf(id)
	if r.Terminal == nil {
		return nil
	}
	t, err := f.hc().Get(context.Background(), r.Terminal.Name)
	if err != nil {
		f.t.Fatal(err)
	}
	return t
}

// waitAgent waits until id's step runs in a live terminal.
func (f *cliFixture) waitAgent(id, step string) {
	f.t.Helper()
	waitFor(f.t, id+" "+step+" in a live terminal", func() bool {
		r := f.runOf(id)
		return r.State == runs.Running && r.Step == step && f.terminalOf(id).Live()
	})
}

// liveTerminals counts the fake hc's live terminals.
func (f *cliFixture) liveTerminals() int {
	f.t.Helper()
	entries, _ := os.ReadDir(f.env.HCDir)
	n := 0
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".json"); ok {
			if t, err := f.hc().Get(context.Background(), name); err == nil && t.Live() {
				n++
			}
		}
	}
	return n
}

type termStatus struct {
	Change, State, Step, Reason, AgentBackend, AgentState, Terminal string
	Round                                                           int
	NotWatched                                                      bool
}

func (f *cliFixture) termStatus(id string) termStatus {
	f.t.Helper()
	res := f.tb("status", "--json")
	var doc struct {
		Runs []struct {
			Change, State, Step, Reason, Terminal string
			Round                                 int
			AgentBackend                          string `json:"agent_backend"`
			AgentState                            string `json:"agent_state"`
			NotWatched                            bool   `json:"not_watched"`
		}
	}
	if err := json.Unmarshal([]byte(res.stdout), &doc); err != nil {
		f.t.Fatalf("status: %v\n%s%s", err, res.stdout, res.stderr)
	}
	for _, r := range doc.Runs {
		if r.Change == id {
			return termStatus{r.Change, r.State, r.Step, r.Reason, r.AgentBackend, r.AgentState, r.Terminal, r.Round, r.NotWatched}
		}
	}
	return termStatus{}
}

// start runs `tuckerbox run id` and fails the test on error.
func (f *cliFixture) start(id string) {
	f.t.Helper()
	if res := f.tb("run", id); res.code != 0 {
		f.t.Fatalf("run %s: exit %d\n%s%s", id, res.code, res.stdout, res.stderr)
	}
}

// startsOf returns the fake agent's starts of id.
func (f *cliFixture) startsOf(id string) []string {
	var out []string
	for _, s := range f.env.Starts(f.t) {
		if strings.HasPrefix(s, id+" ") {
			out = append(out, s)
		}
	}
	return out
}

// Scenario: Agent tab in Herdr, through tuckerbox run: the build step runs
// in a terminal, its address is in run.json and status, and it carries on
// to ready with the review in the same terminal name.
func TestTerminalRun(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.plan(map[string]testenv.Action{"build": {WaitFor: f.gate("build"), ExitAfter: true}})
	f.start("add-login")
	f.waitAgent("add-login", "build")
	r := f.runOf("add-login")
	if r.AgentBackend != "terminal" || r.Terminal.Address == "" || r.Slot == nil || *r.Slot != 0 {
		t.Errorf("run.json: backend %q terminal %+v slot %v", r.AgentBackend, r.Terminal, r.Slot)
	}
	term := f.terminalOf("add-login")
	if term.Labels["herdr.space"] != "repo" || term.Labels["herdr.tab"] != "add-login · build" || !sameDirs(term.Cwd, f.worktreeOf("add-login")) {
		t.Errorf("terminal %+v", term)
	}
	if st := f.termStatus("add-login"); st.State != "running" || st.Terminal != r.Terminal.Address || st.AgentBackend != "terminal" || st.NotWatched {
		t.Errorf("status %+v", st)
	}
	f.open("build")
	f.waitState("add-login", "ready", "")
	if got := f.startsOf("add-login"); len(got) != 2 {
		t.Errorf("agent starts %v", got)
	}
	if r := f.runOf("add-login"); r.Terminal != nil || r.Slot != nil {
		t.Errorf("ready run keeps terminal %+v slot %v", r.Terminal, r.Slot)
	}
}

func sameDirs(a, b string) bool {
	ea, _ := filepath.EvalSymlinks(a)
	eb, _ := filepath.EvalSymlinks(b)
	return ea == eb
}

// Scenario: Custom agent without an interactive command. The terminal
// backend is in use and the step's agent has only command: the step runs
// headless and its backend is headless.
func TestCustomAgentWithoutInteractiveCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake hc has no terminals on Windows")
	}
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	machineConfig(t, f.env.Home, "agent_backend = \"terminal\"\n")
	t.Setenv("FAKEHC_SERVER", "up")
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: f.gate("build")}})
	f.start("add-login")
	f.waitState("add-login", "running", "build")
	waitFor(t, "the agent's pid", func() bool { return f.runOf("add-login").AgentPid > 0 })
	if st := f.termStatus("add-login"); st.AgentBackend != "headless" || st.Terminal != "" {
		t.Errorf("status %+v", st)
	}
	f.open("build")
	f.waitState("add-login", "ready", "")
	if n := f.liveTerminals(); n != 0 {
		t.Errorf("%d terminals", n)
	}
	data, _ := os.ReadFile(filepath.Join(f.env.HCDir, "calls.log"))
	if strings.Contains(string(data), `["run"`) {
		t.Errorf("a terminal was started:\n%s", data)
	}
}

// Scenario: Stop after a crash. The run process of add-login died while
// its build agent kept working in a terminal; tuckerbox stop ends the
// terminal, the run is stopped, and no agent of add-login is live.
func TestStopAfterACrash(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.plan(map[string]testenv.Action{"build": {WaitFor: f.gate("never")}})
	f.start("add-login")
	f.waitAgent("add-login", "build")
	f.kill("add-login")

	// Crash detection with a live terminal: running, not watched.
	if st := f.termStatus("add-login"); st.State != "running" || !st.NotWatched || st.Reason != runs.NotWatched {
		t.Errorf("status after the crash %+v", st)
	}
	if res := f.tb("status"); !strings.Contains(res.stdout, "running (not watched)") {
		t.Errorf("status table:\n%s", res.stdout)
	}
	name := f.runOf("add-login").Terminal.Name

	res := f.tb("stop", "add-login")
	if res.code != 0 || !strings.Contains(res.stdout, "ended the agent's terminal") {
		t.Fatalf("stop: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	r := f.runOf("add-login")
	if r.State != runs.Stopped || r.Reason != runs.StoppedOrphan || r.Terminal != nil {
		t.Errorf("run %s %q terminal %+v", r.State, r.Reason, r.Terminal)
	}
	if term, _ := f.hc().Get(context.Background(), name); term.Live() {
		t.Error("the agent's terminal is still live")
	}
	if n := f.liveTerminals(); n != 0 {
		t.Errorf("%d live terminals", n)
	}
	if st := f.termStatus("add-login"); st.State != "stopped" || st.NotWatched {
		t.Errorf("status after stop %+v", st)
	}
	events, err := history.Read(history.File(mustState(t, f.worktreeOf("add-login"))))
	if err != nil {
		t.Fatal(err)
	}
	if last := events[len(events)-1]; last.Kind != history.Stopped || last.Reason != runs.StoppedOrphan {
		t.Errorf("last event %+v", last)
	}
}

// Stop with the run process alive: it ends its agent's terminal itself.
func TestStopEndsTheAgentTerminal(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.plan(map[string]testenv.Action{"build": {WaitFor: f.gate("never")}})
	f.start("add-login")
	f.waitAgent("add-login", "build")
	name := f.runOf("add-login").Terminal.Name
	if res := f.tb("stop", "add-login"); res.code != 0 {
		t.Fatalf("stop: %s%s", res.stdout, res.stderr)
	}
	if r := f.runOf("add-login"); r.State != runs.Stopped || !strings.Contains(r.Reason, "tuckerbox stop") {
		t.Errorf("run %s %q", r.State, r.Reason)
	}
	if term, _ := f.hc().Get(context.Background(), name); term.Live() {
		t.Error("the agent's terminal is still live")
	}
}

// Scenario: Resume after the run process died. The run process was killed
// while the build agent kept working in round 1; tuckerbox resume adopts
// it: no new agent starts, the run stays in round 1, and the agent's
// result is accepted when its turn ends.
func TestResumeAfterTheRunProcessDied(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.plan(map[string]testenv.Action{"build": {WaitFor: f.gate("build"), ExitAfter: true}})
	f.start("add-login")
	f.waitAgent("add-login", "build")
	addr := f.runOf("add-login").Terminal.Address
	f.kill("add-login")

	if res := f.tb("resume", "add-login"); res.code != 0 {
		t.Fatalf("resume: %s%s", res.stdout, res.stderr)
	}
	waitFor(t, "the resumed run to hold the lock", func() bool {
		return runs.Active(mustState(t, f.worktreeOf("add-login")).RunLock())
	})
	r := f.runOf("add-login")
	if r.Round != 1 || r.Step != "build" || r.State != runs.Running {
		t.Errorf("after resume: %s %s round %d", r.State, r.Step, r.Round)
	}
	f.open("build")
	f.waitState("add-login", "ready", "")
	if got := f.startsOf("add-login"); len(got) != 2 || got[0] != "add-login build 1" || got[1] != "add-login review 1" {
		t.Errorf("agent starts %v, want build once (adopted) and review", got)
	}
	r = f.runOf("add-login")
	if len(r.History) == 0 || r.History[0].Step != "build" || r.History[0].Round != 1 {
		t.Errorf("history %+v", r.History)
	}
	log := readLog(t, mustState(t, f.worktreeOf("add-login")).Log("build"))
	if !strings.Contains(log, "adopted live terminal") || !strings.Contains(log, "("+addr+")") {
		t.Errorf("build log does not show the adoption:\n%s", log)
	}
}

func readLog(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Scenario: Crash, then another change queued. max_parallel = 1;
// add-login's run process dies while its build agent keeps working, and
// add-search is queued: it stays queued while that agent lives, and
// starts once the agent is stopped.
func TestCrashThenAnotherChangeQueued(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login", "add-search"}, MaxParallel: 1})
	f.plan(map[string]testenv.Action{"add-login:build": {WaitFor: f.gate("never")}})
	f.start("add-login")
	f.waitAgent("add-login", "build")
	f.kill("add-login")

	f.start("add-search")
	f.waitState("add-search", "queued", "")
	time.Sleep(1500 * time.Millisecond)
	if s, _ := f.state("add-search"); s != "queued" {
		t.Fatalf("add-search is %s while add-login's orphaned agent holds the only slot", s)
	}
	if got := f.startsOf("add-search"); len(got) != 0 {
		t.Fatalf("add-search's agent started: %v", got)
	}
	if res := f.tb("stop", "add-login"); res.code != 0 {
		t.Fatalf("stop: %s%s", res.stdout, res.stderr)
	}
	f.waitState("add-search", "ready", "")
}

// Scenario: Agent ends while orphaned. An orphaned agent's turn ends and
// its program exits: its agent lock is free, and the queued change takes
// the slot.
func TestAgentEndsWhileOrphaned(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login", "add-search"}, MaxParallel: 1})
	f.plan(map[string]testenv.Action{"add-login:build": {WaitFor: f.gate("build"), ExitAfter: true}})
	f.start("add-login")
	f.waitAgent("add-login", "build")
	f.kill("add-login")
	common := filepath.Join(f.repo, ".git", "tuckerbox")
	if _, err := runs.HoldAgentLock(common, 0, 0); err == nil {
		t.Fatal("slot 0's agent lock is free while the orphaned agent lives")
	}
	f.start("add-search")
	f.waitState("add-search", "queued", "")

	f.open("build") // the orphan's turn ends with its result, and it exits
	waitFor(t, "the orphan's terminal to end", func() bool { return !f.terminalOf("add-login").Live() })
	f.waitState("add-search", "ready", "")
	if s, _ := f.state("add-login"); s != "stopped" {
		t.Errorf("add-login is %s", s)
	}
}

// Scenario: Crash with room for another change. max_parallel = 2;
// add-login's run process dies while its agent keeps working, and
// add-search takes the other slot: the two agents are the only ones
// running.
func TestCrashWithRoomForAnotherChange(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login", "add-search", "add-third"}, MaxParallel: 2})
	f.plan(map[string]testenv.Action{
		"add-login:build":  {WaitFor: f.gate("a"), ExitAfter: true},
		"add-search:build": {WaitFor: f.gate("b"), ExitAfter: true},
	})
	f.start("add-login")
	f.waitAgent("add-login", "build")
	f.kill("add-login")

	f.start("add-search")
	f.waitAgent("add-search", "build")
	if r := f.runOf("add-search"); r.Slot == nil || *r.Slot != 1 {
		t.Errorf("add-search's slot %v, want 1 (slot 0 is the orphan's)", r.Slot)
	}
	f.start("add-third")
	f.waitState("add-third", "queued", "")
	time.Sleep(1 * time.Second)
	if s, _ := f.state("add-third"); s != "queued" {
		t.Errorf("add-third is %s with two agents live", s)
	}
	if n := f.liveTerminals(); n != 2 {
		t.Errorf("%d live agents, want 2", n)
	}
}

// Scenario: Crash, another change admitted, then adoption. max_parallel =
// 2; add-login's run process dies, add-search is admitted to the other
// slot, and tuckerbox resume add-login takes add-login's own slot: no
// third agent starts, and both runs finish within the limit.
func TestCrashAnotherAdmittedThenAdoption(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login", "add-search", "add-third"}, MaxParallel: 2})
	f.plan(map[string]testenv.Action{
		"add-login:build":  {WaitFor: f.gate("a"), ExitAfter: true},
		"add-search:build": {WaitFor: f.gate("b"), ExitAfter: true},
		"add-third:build":  {WaitFor: f.gate("c"), ExitAfter: true},
	})
	f.start("add-login")
	f.waitAgent("add-login", "build")
	f.kill("add-login")
	f.start("add-search")
	f.waitAgent("add-search", "build")

	if res := f.tb("resume", "add-login"); res.code != 0 {
		t.Fatalf("resume: %s%s", res.stdout, res.stderr)
	}
	waitFor(t, "add-login's run process", func() bool {
		return runs.Active(mustState(t, f.worktreeOf("add-login")).RunLock())
	})
	f.start("add-third")
	f.waitState("add-third", "queued", "")
	time.Sleep(1 * time.Second)
	if s, _ := f.state("add-third"); s != "queued" {
		t.Errorf("add-third is %s with two agents live", s)
	}
	if n := f.liveTerminals(); n != 2 {
		t.Errorf("%d live agents, want 2", n)
	}
	if r := f.runOf("add-login"); r.Slot == nil || *r.Slot != 0 || r.Round != 1 {
		t.Errorf("add-login slot %v round %d, want its own slot 0 in round 1", r.Slot, r.Round)
	}
	if got := f.startsOf("add-login"); len(got) != 1 {
		t.Errorf("add-login agent starts %v, want the adopted one only", got)
	}
	// Both finish; the third then runs; never more than two agents.
	stop := make(chan struct{})
	most := make(chan int, 1)
	go func() {
		m := 0
		for {
			select {
			case <-stop:
				most <- m
				return
			default:
			}
			if n := f.liveTerminals(); n > m {
				m = n
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	f.open("a")
	f.open("b")
	f.waitState("add-login", "ready", "")
	f.waitState("add-search", "ready", "")
	f.open("c")
	f.waitState("add-third", "ready", "")
	close(stop)
	if m := <-most; m > 2 {
		t.Errorf("%d agents were live at once, max_parallel is 2", m)
	}
}

// Scenario: Logs after the step. A terminal step finished and its terminal
// was later replaced by the next step's: tuckerbox logs add-login build
// shows the build agent's session from the log file. While a step's
// terminal lives, logs reads it through hc.
func TestLogsAfterTheStep(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.plan(map[string]testenv.Action{
		"build":  {Ask: "Which design?", ExitAfter: true},
		"review": {WaitFor: f.gate("review"), ExitAfter: true},
	})
	f.start("add-login")
	f.waitAgent("add-login", "build")
	waitFor(t, "the build agent's question", func() bool { return f.runOf("add-login").AgentState == agent.AgentWaiting })
	if st := f.termStatus("add-login"); st.AgentState != "waiting for you" {
		t.Errorf("status %+v", st)
	}
	// While the terminal lives, logs reads it through hc.
	res := f.tb("logs", "add-login", "build")
	if res.code != 0 || !strings.Contains(res.stdout, "Which design?") {
		t.Errorf("logs of the live terminal: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	// Following it shows what the agent writes next, until its terminal
	// ends.
	follow := exec.Command(tools.Tuckerbox, "logs", "add-login", "build", "--follow")
	follow.Dir = f.repo
	var followed lockedBuffer
	follow.Stdout, follow.Stderr = &followed, &followed
	if err := follow.Start(); err != nil {
		t.Fatal(err)
	}
	followDone := make(chan error, 1)
	go func() { followDone <- follow.Wait() }()
	time.Sleep(700 * time.Millisecond)
	if err := f.hc().Send(context.Background(), f.runOf("add-login").Terminal.Name, "the second"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-followDone:
		if err != nil {
			t.Errorf("logs --follow: %v\n%s", err, followed.String())
		}
	case <-time.After(30 * time.Second):
		_ = follow.Process.Kill()
		t.Fatalf("logs --follow did not end with the terminal:\n%s", followed.String())
	}
	if out := followed.String(); !strings.Contains(out, "the person said: the second") || !strings.Contains(out, "ended; tuckerbox logs add-login build shows its session") {
		t.Errorf("logs --follow:\n%s", out)
	}
	f.waitAgent("add-login", "review") // the build's terminal is replaced by the review's
	res = f.tb("logs", "add-login", "build")
	for _, want := range []string{"Which design?", "the person said: the second", "history of terminal"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("logs add-login build lacks %q:\n%s%s", want, res.stdout, res.stderr)
		}
	}
	if r := f.runOf("add-login"); r.History[0].Summary != "answer: the second" {
		t.Errorf("build summary %q", r.History[0].Summary)
	}
	f.open("review")
	f.waitState("add-login", "ready", "")
}

// Autopilot's crash resume adopts a live agent too: no new round, no
// second agent.
func TestAutopilotAdoptsALiveAgent(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	f.approveAll("add-login")
	f.plan(map[string]testenv.Action{"build": {WaitFor: f.gate("build"), ExitAfter: true}})
	f.pass()
	f.waitAgent("add-login", "build")
	f.kill("add-login")

	if res := f.pass(); !strings.Contains(res.stdout, "resume add-login (its run crashed)") {
		t.Fatalf("pass after the crash:\n%s", res.stdout)
	}
	waitFor(t, "the resumed run", func() bool {
		return runs.Active(mustState(t, f.worktreeOf("add-login")).RunLock())
	})
	if r := f.runOf("add-login"); r.Round != 1 || r.Step != "build" {
		t.Errorf("after autopilot's resume: %s round %d", r.Step, r.Round)
	}
	f.open("build")
	waitFor(t, "the build's result", func() bool {
		r := f.runOf("add-login")
		return len(r.History) > 0
	})
	if r := f.runOf("add-login"); r.History[0].Step != "build" || r.History[0].Round != 1 {
		t.Errorf("history %+v", r.History)
	}
	if got := f.startsOf("add-login"); len(got) < 1 || got[0] != "add-login build 1" || (len(got) > 1 && strings.HasPrefix(got[1], "add-login build")) {
		t.Errorf("agent starts %v, want build once", got)
	}
}

// agent-hold takes its slot's agent lock for as long as its agent runs,
// refuses to start an agent while another holds it, and passes its
// agent's exit status on.
func TestAgentHold(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	common := t.TempDir()
	gate := filepath.Join(t.TempDir(), "gate")
	held := filepath.Join(t.TempDir(), "held")
	done := make(chan cmdResult, 1)
	go func() {
		done <- tbIn(t, common, "agent-hold", "--slot", "1", "--common", common, "--env", "HOLD_TEST=yes", "--",
			"sh", "-c", `echo "$HOLD_TEST" > "$1"; while [ ! -e "$2" ]; do sleep 0.05; done; exit 7`, "sh", held, gate)
	}()
	waitFor(t, "the held agent to start", func() bool {
		data, _ := os.ReadFile(held)
		return strings.TrimSpace(string(data)) == "yes"
	})
	if _, err := runs.HoldAgentLock(common, 1, 0); err == nil {
		t.Fatal("slot 1's agent lock is free while agent-hold runs")
	}
	if s, _ := runs.TrySlot(common, 2); s == nil || s.N != 0 {
		t.Errorf("admission took slot %v, want 0 (slot 1's agent lives)", s)
	} else {
		s.Release()
	}
	res := tbIn(t, common, "agent-hold", "--slot", "1", "--common", common, "--", "sh", "-c", "exit 0")
	if res.code != 1 || !strings.Contains(res.stderr, "slot 1's agent lock is held") {
		t.Errorf("second agent-hold: exit %d %s", res.code, res.stderr)
	}
	testenv.WriteFile(t, gate, "")
	if res := <-done; res.code != 7 {
		t.Errorf("agent-hold exit %d, want the agent's 7\n%s", res.code, res.stderr)
	}
	h, err := runs.HoldAgentLock(common, 1, 0)
	if err != nil {
		t.Fatalf("the agent lock is still held after its agent ended: %v", err)
	}
	h.Release()
}

// doctor's turn-hook self-check, for each installed agent CLI.
func TestDoctorTurnHookCheck(t *testing.T) {
	a, out, _ := app("claude", "codex")
	a.DoctorEnv.Output = func(ctx context.Context, path string, args ...string) ([]byte, error) {
		if len(args) == 1 && args[0] == "--help" {
			if strings.HasSuffix(path, "codex") {
				return []byte("  -c, --config <key=value>\n"), nil
			}
			return []byte("  --model <model>\n"), nil // a claude without --settings
		}
		return []byte("1.0.0\n"), nil
	}
	a.Run([]string{"doctor"})
	for _, want := range []string{
		"turn hook: claude: claude --help does not list --settings",
		"turn hook: codex ok (notify through -c)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("doctor lacks %q:\n%s", want, out.String())
		}
	}
}
