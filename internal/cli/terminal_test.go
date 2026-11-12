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

	"github.com/gofrs/flock"

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
	// agent-hold is given the run's claim on its slot.
	if argv := f.terminalArgv(r.Terminal.Name); !hasPair(argv, "--owner", runs.SlotOwner(f.common(), 0)) || runs.SlotOwner(f.common(), 0) == "" {
		t.Errorf("terminal program %v lacks --owner %q", argv, runs.SlotOwner(f.common(), 0))
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

// Logs after a crash, stop and resume: stopping the orphaned agent saves
// its terminal's history to the step log, so logs shows its session at
// once, and still does after resume starts the step's next agent in the
// same terminal name.
func TestLogsAfterCrashStopAndResume(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.plan(map[string]testenv.Action{"build": {Ask: "Which design?", ExitAfter: true}})
	f.start("add-login")
	f.waitAgent("add-login", "build")
	waitFor(t, "the build agent's question", func() bool { return f.runOf("add-login").AgentState == agent.AgentWaiting })
	f.kill("add-login")
	if res := f.tb("stop", "add-login"); res.code != 0 {
		t.Fatalf("stop: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	st := mustState(t, f.worktreeOf("add-login"))
	want := []string{"history of terminal", "fakeagent: add-login build round 1", "Which design?"}
	res := f.tb("logs", "add-login", "build")
	for _, w := range want {
		if !strings.Contains(res.stdout, w) {
			t.Errorf("logs after the stop lacks %q:\n%s%s", w, res.stdout, res.stderr)
		}
	}

	f.plan(map[string]testenv.Action{"build": {WaitFor: f.gate("build2"), ExitAfter: true}})
	if res := f.tb("resume", "add-login"); res.code != 0 {
		t.Fatalf("resume: %s%s", res.stdout, res.stderr)
	}
	waitFor(t, "the resumed build agent", func() bool { return len(f.startsOf("add-login")) == 2 })
	f.waitAgent("add-login", "build")
	log := readLog(t, st.Log("build"))
	for _, w := range want {
		if !strings.Contains(log, w) {
			t.Errorf("build log after resume lacks %q:\n%s", w, log)
		}
	}
	f.open("build2")
	waitFor(t, "the resumed build to finish", func() bool { return len(f.runOf("add-login").History) > 0 })
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
		"turn hook: codex: unverified: agent-turn reads the notify hook payload Tuckerbox expects and codex --help lists --config",
		"only seen in a real turn",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("doctor lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "turn hook: codex ok") {
		t.Errorf("doctor claims codex's hook works without a real turn:\n%s", out.String())
	}
}

// common is the repository's common Tuckerbox directory (slot locks).
func (f *cliFixture) common() string { return filepath.Join(f.repo, ".git", "tuckerbox") }

// terminalArgv is the program the fake hc's terminal called name runs.
func (f *cliFixture) terminalArgv(name string) []string {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.env.HCDir, name+".json"))
	if err != nil {
		f.t.Fatal(err)
	}
	var rec struct{ Argv []string }
	if err := json.Unmarshal(data, &rec); err != nil {
		f.t.Fatal(err)
	}
	return rec.Argv
}

func hasPair(argv []string, flag, value string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}

// fault sets a fake hc fault file (see testdata/fakehc); "" clears it.
func (f *cliFixture) fault(name, value string) {
	f.t.Helper()
	p := filepath.Join(f.env.HCDir, name)
	if value == "" {
		_ = os.Remove(p)
		return
	}
	testenv.WriteFile(f.t, p, value)
}

// After a crash with max_parallel = 1, the orphaned agent holds the only
// slot's agent lock. A rerun (a new round, not an adoption) ends that
// terminal under the change lock before it is admitted, rather than
// waiting forever for the slot its own old agent holds.
func TestRerunAfterACrashEndsTheOldAgent(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, MaxParallel: 1})
	f.plan(map[string]testenv.Action{
		"build:1": {WaitFor: f.gate("never")},
		"build":   {ExitAfter: true},
	})
	f.start("add-login")
	f.waitAgent("add-login", "build")
	waitFor(t, "the round 1 agent", func() bool { return len(f.startsOf("add-login")) == 1 })
	f.kill("add-login")
	if _, err := runs.HoldAgentLock(f.common(), 0, 0); err == nil {
		t.Fatal("slot 0's agent lock is free while the orphaned agent lives")
	}

	res := f.tb("rerun", "add-login", "build")
	if res.code != 0 {
		t.Fatalf("rerun: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout+res.stderr, "ending the agent left in terminal") {
		t.Errorf("rerun does not say the old agent was ended:\n%s%s", res.stdout, res.stderr)
	}
	f.waitState("add-login", "ready", "")
	got := f.startsOf("add-login")
	if len(got) != 3 || got[0] != "add-login build 1" || got[1] != "add-login build 2" || got[2] != "add-login review 2" {
		t.Errorf("agent starts %v, want build 1, then build 2 and review 2", got)
	}
}

// Crash at the first boundary: the run process dies while hc is starting
// the agent's terminal, before it hears the address. run.json named the
// terminal before hc run, so status shows the agent not watched and
// resume adopts it in the same round.
func TestCrashWhileTheTerminalStarts(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.plan(map[string]testenv.Action{"build": {WaitFor: f.gate("build"), ExitAfter: true}})
	f.fault("run-delay", "3s")
	f.start("add-login")
	var name string
	waitFor(t, "run.json to name the terminal before hc run answers", func() bool {
		r := f.runOf("add-login")
		if r.State == runs.Running && r.Terminal != nil && r.Terminal.Address == "" {
			name = r.Terminal.Name
			return true
		}
		return false
	})
	waitFor(t, "the terminal to be live", func() bool {
		term, _ := f.hc().Get(context.Background(), name)
		return term.Live()
	})
	f.fault("run-delay", "")
	f.kill("add-login")
	if r := f.runOf("add-login"); r.Terminal == nil || r.Terminal.Address != "" {
		t.Fatalf("the run process heard the address before it was killed (%+v); the test missed the boundary", r.Terminal)
	}
	if st := f.termStatus("add-login"); st.State != "running" || !st.NotWatched {
		t.Errorf("status after the crash %+v", st)
	}

	if res := f.tb("resume", "add-login"); res.code != 0 {
		t.Fatalf("resume: %s%s", res.stdout, res.stderr)
	}
	waitFor(t, "the adopted terminal's address", func() bool {
		r := f.runOf("add-login")
		return r.Terminal != nil && r.Terminal.Address != "" && runs.Active(mustState(t, f.worktreeOf("add-login")).RunLock())
	})
	if r := f.runOf("add-login"); r.Round != 1 {
		t.Errorf("round %d after resume, want 1 (adopted)", r.Round)
	}
	f.open("build")
	f.waitState("add-login", "ready", "")
	if got := f.startsOf("add-login"); len(got) != 2 || got[0] != "add-login build 1" {
		t.Errorf("agent starts %v, want build once and review", got)
	}
}

// Crash at the second boundary: a resume that adopts the orphaned agent
// writes queued, then dies while it waits for its slot's run lock (held
// for a moment by an admission). The run still names its terminal and
// slot: status shows it not watched, and the next resume adopts it.
func TestCrashWhileAdoptionTakesTheSlot(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, MaxParallel: 1})
	f.plan(map[string]testenv.Action{"build": {WaitFor: f.gate("build"), ExitAfter: true}})
	f.start("add-login")
	f.waitAgent("add-login", "build")
	f.kill("add-login")

	// An admission testing slot 0 holds its run lock.
	admission := flock.New(runs.SlotLock(f.common(), 0))
	if ok, err := admission.TryLock(); !ok || err != nil {
		t.Fatalf("slot 0's run lock: %v %v", ok, err)
	}
	defer admission.Unlock()
	if res := f.tb("resume", "add-login"); res.code != 0 {
		t.Fatalf("resume: %s%s", res.stdout, res.stderr)
	}
	waitFor(t, "the adopting resume to wait for its slot", func() bool {
		return runs.Active(mustState(t, f.worktreeOf("add-login")).RunLock()) && f.runOf("add-login").State == runs.Queued
	})
	f.kill("add-login")
	r := f.runOf("add-login")
	if r.State != runs.Queued || r.Terminal == nil || r.Slot == nil || *r.Slot != 0 || r.Round != 1 {
		t.Fatalf("run after the adoption crashed: %s round %d terminal %+v slot %v", r.State, r.Round, r.Terminal, r.Slot)
	}
	if st := f.termStatus("add-login"); st.State != "running" || !st.NotWatched {
		t.Errorf("status %+v", st)
	}
	_ = admission.Unlock()

	if res := f.tb("resume", "add-login"); res.code != 0 {
		t.Fatalf("second resume: %s%s", res.stdout, res.stderr)
	}
	waitFor(t, "the adopting run in slot 0", func() bool {
		r := f.runOf("add-login")
		return r.State == runs.Running && r.Slot != nil && *r.Slot == 0
	})
	if r := f.runOf("add-login"); r.Round != 1 {
		t.Errorf("round %d, want 1 (adopted)", r.Round)
	}
	f.open("build")
	f.waitState("add-login", "ready", "")
	if got := f.startsOf("add-login"); len(got) != 2 || got[0] != "add-login build 1" {
		t.Errorf("agent starts %v, want build once (adopted) and review", got)
	}
}

// Stop while a resume waits to take its adopted agent's slot back (another
// process holds slot 0's run lock): the resume ends the agent's terminal,
// and knows it is gone, before it records stopped. When the terminal will
// not end, nothing is recorded stopped and the run keeps its terminal and
// slot, so a later stop ends the agent.
func TestStopWhileAdoptionTakesTheSlot(t *testing.T) {
	for _, mode := range []string{"", "fail"} {
		t.Run("kill="+mode, func(t *testing.T) {
			f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, MaxParallel: 1})
			f.plan(map[string]testenv.Action{"build": {WaitFor: f.gate("never")}})
			f.start("add-login")
			f.waitAgent("add-login", "build")
			name := f.runOf("add-login").Terminal.Name
			f.kill("add-login")

			admission := flock.New(runs.SlotLock(f.common(), 0))
			if ok, err := admission.TryLock(); !ok || err != nil {
				t.Fatalf("slot 0's run lock: %v %v", ok, err)
			}
			defer admission.Unlock()
			if res := f.tb("resume", "add-login"); res.code != 0 {
				t.Fatalf("resume: %s%s", res.stdout, res.stderr)
			}
			waitFor(t, "the adopting resume to wait for its slot", func() bool {
				return runs.Active(mustState(t, f.worktreeOf("add-login")).RunLock()) && f.runOf("add-login").State == runs.Queued
			})

			stopsBefore := f.stoppedEvents("add-login")
			f.fault("kill-mode", mode)
			res := f.tb("stop", "add-login")
			term, _ := f.hc().Get(context.Background(), name)
			r := f.runOf("add-login")
			if mode == "fail" {
				if res.code == 0 {
					t.Errorf("stop with a failing hc kill: exit 0\n%s", res.stdout)
				}
				if r.State == runs.Stopped || r.Terminal == nil || r.Terminal.Name != name || r.Slot == nil || *r.Slot != 0 {
					t.Errorf("run after the failed stop: %s terminal %+v slot %v", r.State, r.Terminal, r.Slot)
				}
				if !term.Live() {
					t.Fatal("the fault did not keep the terminal live")
				}
				if n := f.stoppedEvents("add-login"); n != stopsBefore {
					t.Errorf("stopped recorded while the agent lives (%d stopped events, %d before)", n, stopsBefore)
				}
				f.fault("kill-mode", "")
				if res := f.tb("stop", "add-login"); res.code != 0 {
					t.Fatalf("second stop: exit %d\n%s%s", res.code, res.stdout, res.stderr)
				}
				term, _ = f.hc().Get(context.Background(), name)
				r = f.runOf("add-login")
			} else if res.code != 0 {
				t.Errorf("stop: exit %d\n%s%s", res.code, res.stdout, res.stderr)
			}
			if r.State != runs.Stopped || r.Terminal != nil {
				t.Errorf("run %s %q terminal %+v", r.State, r.Reason, r.Terminal)
			}
			if term.Live() {
				t.Error("the agent's terminal is still live after stop")
			}
			if n := f.liveTerminals(); n != 0 {
				t.Errorf("%d live terminals", n)
			}

			// The agent's session is in the step log at once, and still is
			// after resume starts the step's next agent in a terminal of
			// the same name.
			want := []string{"history of terminal", "fakeagent: add-login build round 1"}
			res = f.tb("logs", "add-login", "build")
			for _, w := range want {
				if !strings.Contains(res.stdout, w) {
					t.Errorf("logs after the stop lacks %q:\n%s%s", w, res.stdout, res.stderr)
				}
			}
			_ = admission.Unlock()
			f.plan(map[string]testenv.Action{"build": {WaitFor: f.gate("build2"), ExitAfter: true}})
			if res := f.tb("resume", "add-login"); res.code != 0 {
				t.Fatalf("resume: %s%s", res.stdout, res.stderr)
			}
			waitFor(t, "the resumed build agent", func() bool { return len(f.startsOf("add-login")) == 2 })
			f.waitAgent("add-login", "build")
			log := readLog(t, mustState(t, f.worktreeOf("add-login")).Log("build"))
			for _, w := range want {
				if !strings.Contains(log, w) {
					t.Errorf("build log after resume lacks %q:\n%s", w, log)
				}
			}
			f.open("build2")
			waitFor(t, "the resumed build to finish", func() bool { return len(f.runOf("add-login").History) > 0 })
		})
	}
}

// A resume that adopts an orphaned agent, but whose step cannot then look
// its terminal up (herdr-core stops answering), does not record the step
// failed and forget the terminal: the run stays running with its terminal
// and slot, and tuckerbox stop ends the agent.
func TestAdoptionLookupFailsThenStop(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, MaxParallel: 1})
	f.plan(map[string]testenv.Action{"build": {WaitFor: f.gate("never")}})
	f.start("add-login")
	f.waitAgent("add-login", "build")
	name := f.runOf("add-login").Terminal.Name
	f.kill("add-login")

	// One get answers (resume finds the agent to adopt), then none do.
	f.fault("get-fail", "1")
	res := f.tb("resume", "add-login")
	st := mustState(t, f.worktreeOf("add-login"))
	waitFor(t, "the resume to give up on the lookup", func() bool { return !runs.Active(st.RunLock()) })
	f.fault("get-fail", "")
	r := f.runOf("add-login")
	if r.State != runs.Running || r.Terminal == nil || r.Terminal.Name != name || r.Slot == nil || r.Round != 1 {
		t.Fatalf("run after the failed lookup: %s %q round %d terminal %+v slot %v\n%s%s",
			r.State, r.Reason, r.Round, r.Terminal, r.Slot, res.stdout, res.stderr)
	}
	if term, _ := f.hc().Get(context.Background(), name); !term.Live() {
		t.Fatal("the agent's terminal is not live")
	}
	if res := f.tb("stop", "add-login"); res.code != 0 {
		t.Fatalf("stop: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	r = f.runOf("add-login")
	if r.State != runs.Stopped || r.Terminal != nil {
		t.Errorf("run %s %q terminal %+v", r.State, r.Reason, r.Terminal)
	}
	if n := f.liveTerminals(); n != 0 {
		t.Errorf("%d live terminals after stop", n)
	}
}

// stoppedEvents counts the change's stopped history events.
func (f *cliFixture) stoppedEvents(id string) int {
	f.t.Helper()
	events, err := history.Read(history.File(mustState(f.t, f.worktreeOf(id))))
	if err != nil {
		f.t.Fatal(err)
	}
	n := 0
	for _, e := range events {
		if e.Kind == history.Stopped {
			n++
		}
	}
	return n
}

// Stop when the run process cannot end its agent's terminal (hc kill
// fails): nothing is recorded stopped, the run keeps its terminal and
// slot, and a later stop, once hc works, ends the agent.
func TestStopWhenTheTerminalWontEnd(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.plan(map[string]testenv.Action{"build": {WaitFor: f.gate("never")}})
	f.start("add-login")
	f.waitAgent("add-login", "build")
	name := f.runOf("add-login").Terminal.Name

	f.fault("kill-mode", "fail")
	res := f.tb("stop", "add-login")
	if res.code == 0 || !strings.Contains(res.stderr, "fake kill failure") {
		t.Errorf("stop with a failing hc kill: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	r := f.runOf("add-login")
	if r.State != runs.Running || r.Terminal == nil || r.Terminal.Name != name || r.Slot == nil {
		t.Errorf("run after the failed stop: %s terminal %+v slot %v", r.State, r.Terminal, r.Slot)
	}
	if term, _ := f.hc().Get(context.Background(), name); !term.Live() {
		t.Fatal("the fault did not keep the terminal live")
	}
	if st := f.termStatus("add-login"); st.State != "running" || !st.NotWatched {
		t.Errorf("status %+v", st)
	}
	events, err := history.Read(history.File(mustState(t, f.worktreeOf("add-login"))))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Kind == history.Stopped {
			t.Errorf("stopped recorded while the agent lives: %+v", e)
		}
	}

	f.fault("kill-mode", "")
	if res := f.tb("stop", "add-login"); res.code != 0 || !strings.Contains(res.stdout, "ended the agent's terminal") {
		t.Fatalf("second stop: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	if r := f.runOf("add-login"); r.State != runs.Stopped || r.Terminal != nil {
		t.Errorf("run %s terminal %+v", r.State, r.Terminal)
	}
	if term, _ := f.hc().Get(context.Background(), name); term.Live() {
		t.Error("the agent's terminal is still live")
	}
}

// A late agent-hold: its run took slot 0 and started the terminal, but
// died before agent-hold took the agent lock, and another run was then
// admitted to slot 0 (both of its locks were free). agent-hold finds the
// slot claimed by that other run and does not start its agent; the
// admitted run's own agent then can. Had agent-hold taken the agent lock
// first, the admission would have found it held instead.
func TestLateAgentHoldAfterTheSlotWasReassigned(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	common := t.TempDir()
	ran := filepath.Join(t.TempDir(), "ran")
	hold := func(owner string) cmdResult {
		return tbIn(t, common, "agent-hold", "--slot", "0", "--common", common, "--owner", owner, "--",
			"sh", "-c", `echo "$0" >> "$1"`, owner, ran)
	}

	a, err := runs.TrySlot(common, 1)
	if err != nil || a == nil {
		t.Fatalf("admitting a: %v %v", a, err)
	}
	a.Release() // a's run process dies; its terminal's agent-hold has not run yet
	b, err := runs.TrySlot(common, 1)
	if err != nil || b == nil {
		t.Fatalf("admitting b with both of slot 0's locks free: %v %v", b, err)
	}
	defer b.Release()
	if a.Owner == b.Owner {
		t.Fatal("two admissions claimed the slot with one token")
	}
	res := hold(a.Owner)
	if res.code == 0 || !strings.Contains(res.stderr, "given to another run") {
		t.Errorf("late agent-hold: exit %d %s", res.code, res.stderr)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("the late agent-hold started its agent beside the admitted run's")
	}
	if res := hold(b.Owner); res.code != 0 {
		t.Errorf("the admitted run's agent-hold: exit %d %s", res.code, res.stderr)
	}
	if data, _ := os.ReadFile(ran); strings.TrimSpace(string(data)) != b.Owner {
		t.Errorf("agents that ran: %q, want only b's", data)
	}

	// The other order: a's agent-hold takes the agent lock while the
	// slot is still a's; a's run process then dies, and admission finds
	// slot 0 taken by the orphaned agent.
	b.Release()
	c, err := runs.TrySlot(common, 1)
	if err != nil || c == nil {
		t.Fatal(err)
	}
	gate := filepath.Join(t.TempDir(), "gate")
	started := filepath.Join(t.TempDir(), "started")
	done := make(chan cmdResult, 1)
	go func() {
		done <- tbIn(t, common, "agent-hold", "--slot", "0", "--common", common, "--owner", c.Owner, "--",
			"sh", "-c", `touch "$0"; while [ ! -e "$1" ]; do sleep 0.05; done`, started, gate)
	}()
	waitFor(t, "c's agent to start", func() bool { _, err := os.Stat(started); return err == nil })
	c.Release()
	if s, _ := runs.TrySlot(common, 1); s != nil {
		s.Release()
		t.Error("admission took slot 0 while c's orphaned agent holds its agent lock")
	}
	testenv.WriteFile(t, gate, "")
	if res := <-done; res.code != 0 {
		t.Errorf("c's agent-hold: exit %d %s", res.code, res.stderr)
	}
}

// Stop while herdr-core has yet to launch the agent's terminal, and
// launches it only after the run gave up looking for it: the run gives its
// slot a fresh claim before it records stopped, so the late terminal's
// agent-hold refuses and no agent runs after the stop.
func TestStopBeforeALateLaunch(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, MaxParallel: 1})
	f.plan(map[string]testenv.Action{"build": {ExitAfter: true}})
	f.fault("launch-later", "4s")
	began := time.Now()
	f.start("add-login")
	var name string
	waitFor(t, "run.json to name the terminal before hc run answers", func() bool {
		r := f.runOf("add-login")
		if r.State == runs.Running && r.Terminal != nil && r.Terminal.Address == "" {
			name = r.Terminal.Name
			return true
		}
		return false
	})
	if res := f.tb("stop", "add-login"); res.code != 0 {
		t.Fatalf("stop: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	st := mustState(t, f.worktreeOf("add-login"))
	waitFor(t, "the run to record stopped", func() bool {
		return !runs.Active(st.RunLock()) && f.runOf("add-login").State == runs.Stopped
	})
	f.fault("launch-later", "")
	if term, _ := f.hc().Get(context.Background(), name); term != nil || time.Since(began) > 4*time.Second {
		t.Fatalf("the terminal launched (%+v) before the run gave up on it; the test missed a late launch", term)
	}
	if r := f.runOf("add-login"); r.Terminal != nil || r.Slot != nil {
		t.Errorf("stopped run keeps terminal %+v slot %v", r.Terminal, r.Slot)
	}
	waitFor(t, "the late terminal to launch and end", func() bool {
		term, _ := f.hc().Get(context.Background(), name)
		return term != nil && !term.Live()
	})
	out, _ := os.ReadFile(filepath.Join(f.env.HCDir, name+".out"))
	if !strings.Contains(string(out), "given to another run") {
		t.Errorf("the late terminal's output %q; want agent-hold to refuse", out)
	}
	if got := f.startsOf("add-login"); len(got) != 0 {
		t.Errorf("agent starts after the stop: %v", got)
	}
	if n := f.liveTerminals(); n != 0 {
		t.Errorf("%d live terminals", n)
	}
}

// The run process dies while herdr-core has yet to launch the agent's
// terminal, and tuckerbox stop settles the run before the terminal
// appears: stop revokes the slot claim the late terminal's agent-hold
// checks before it records stopped, so no agent runs after the stop.
func TestStopOrphanBeforeALateLaunch(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, MaxParallel: 1})
	f.plan(map[string]testenv.Action{"build": {ExitAfter: true}})
	f.fault("launch-later", "5s")
	began := time.Now()
	f.start("add-login")
	var name string
	waitFor(t, "run.json to name the terminal before hc run answers", func() bool {
		r := f.runOf("add-login")
		if r.State == runs.Running && r.Terminal != nil && r.Terminal.Address == "" && r.Slot != nil {
			name = r.Terminal.Name
			return true
		}
		return false
	})
	if r := f.runOf("add-login"); r.Terminal.Owner == "" || r.Terminal.Common == "" {
		t.Fatalf("run.json does not record the terminal's slot claim: %+v", r.Terminal)
	}
	f.kill("add-login")
	if res := f.tb("stop", "add-login"); res.code != 0 {
		t.Fatalf("stop: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	f.fault("launch-later", "")
	if term, _ := f.hc().Get(context.Background(), name); term != nil || time.Since(began) > 5*time.Second {
		t.Fatalf("the terminal launched (%+v) before stop settled the run; the test missed a late launch", term)
	}
	if r := f.runOf("add-login"); r.State != runs.Stopped || r.Terminal != nil || r.Slot != nil {
		t.Errorf("run after stop: %s terminal %+v slot %v", r.State, r.Terminal, r.Slot)
	}
	waitFor(t, "the late terminal to launch and end", func() bool {
		term, _ := f.hc().Get(context.Background(), name)
		return term != nil && !term.Live()
	})
	out, _ := os.ReadFile(filepath.Join(f.env.HCDir, name+".out"))
	if !strings.Contains(string(out), "given to another run") {
		t.Errorf("the late terminal's output %q; want agent-hold to refuse", out)
	}
	if got := f.startsOf("add-login"); len(got) != 0 {
		t.Errorf("agent starts after the stop: %v", got)
	}
	if n := f.liveTerminals(); n != 0 {
		t.Errorf("%d live terminals", n)
	}
}

// The run process dies, and its agent then ends by itself while no
// process watches it. resume starts a new round in a terminal of the same
// name, and first appends the ended terminal's history to the step log,
// so the orphan's session is not lost when the new terminal replaces it.
func TestOrphanEndsThenResumeKeepsItsSession(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, MaxParallel: 1})
	f.plan(map[string]testenv.Action{"build": {WaitFor: f.gate("orphan"), Status: "none", ExitAfter: true}})
	f.start("add-login")
	f.waitAgent("add-login", "build")
	name := f.runOf("add-login").Terminal.Name
	f.kill("add-login")
	f.open("orphan")
	waitFor(t, "the orphaned agent to end", func() bool {
		term, _ := f.hc().Get(context.Background(), name)
		return term != nil && !term.Live()
	})

	f.plan(map[string]testenv.Action{"build": {WaitFor: f.gate("build2"), ExitAfter: true}})
	if res := f.tb("resume", "add-login"); res.code != 0 {
		t.Fatalf("resume: %s%s", res.stdout, res.stderr)
	}
	waitFor(t, "the resumed build agent", func() bool { return len(f.startsOf("add-login")) == 2 })
	f.waitAgent("add-login", "build")
	if r := f.runOf("add-login"); r.Round != 2 {
		t.Errorf("resumed round %d, want 2", r.Round)
	}
	log := readLog(t, mustState(t, f.worktreeOf("add-login")).Log("build"))
	for _, w := range []string{"history of terminal " + name, "fakeagent: add-login build round 1"} {
		if !strings.Contains(log, w) {
			t.Errorf("build log after resume lacks %q:\n%s", w, log)
		}
	}
	f.open("build2")
	waitFor(t, "the resumed build to finish", func() bool { return len(f.runOf("add-login").History) > 0 })
	res := f.tb("logs", "add-login", "build")
	if !strings.Contains(res.stdout, "fakeagent: add-login build round 1") {
		t.Errorf("logs after the step lacks the orphan's session:\n%s%s", res.stdout, res.stderr)
	}
}
