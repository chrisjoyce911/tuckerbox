package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// termFixture runs steps in fake herdr-core terminals.
type termFixture struct {
	t   *testing.T
	env *testenv.Env
	be  Terminal
	hc  HC
}

func newTermFixture(t *testing.T) *termFixture {
	t.Helper()
	if termTools == nil {
		t.Skip("the fake hc has no terminals on Windows")
	}
	env := termTools.Setup(t)
	t.Setenv("FAKEHC_SERVER", "up")
	hc := HC{Path: env.HC}
	return &termFixture{t: t, env: env, hc: hc, be: Terminal{HC: hc, Exe: termTools.Tuckerbox}}
}

// interactiveFake is the plan-driven fake agent with an interactive command.
func interactiveFake() Template {
	return Template{
		Name:        "fake",
		Command:     []string{termTools.FakeAgent, PhBrief},
		Interactive: []string{termTools.FakeAgent, "--notify", PhTurnHook, "--", PhBrief},
	}
}

// step returns a terminal step of change fix-login in repository shop.
func (f *termFixture) step(step, role string, round int, tmpl Template) Step {
	s := newStep(f.t, step, role, round, tmpl)
	s.Term = TermInfo{
		Name: TerminalName("example.com/acme/shop", s.Change), Space: "shop", RunID: "run-1",
		Common: f.t.TempDir(), Slot: 0,
	}
	return s
}

var termOpts = Options{Poll: 30 * time.Millisecond, TermPoll: 60 * time.Millisecond, StopGrace: 2 * time.Second, ResultGrace: 300 * time.Millisecond}

func (f *termFixture) run(s Step, opts Options) Outcome {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	o, err := f.be.Run(ctx, s, opts)
	if err != nil {
		f.t.Fatal(err)
	}
	return o
}

// calls returns the fake hc's calls so far.
func (f *termFixture) calls() [][]string {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.env.HCDir, "calls.log"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	var out [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var c []string
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

// runCalls returns the hc run calls.
func (f *termFixture) runCalls() [][]string {
	var out [][]string
	for _, c := range f.calls() {
		if c[0] == "run" {
			out = append(out, c)
		}
	}
	return out
}

func (f *termFixture) get(name string) *HCTerminal {
	f.t.Helper()
	term, err := f.hc.Get(context.Background(), name)
	if err != nil {
		f.t.Fatal(err)
	}
	return term
}

// labels returns an hc run call's labels.
func labels(call []string) map[string]string {
	out := map[string]string{}
	for i := 0; i+1 < len(call); i++ {
		if call[i] == "--label" {
			k, v, _ := strings.Cut(call[i+1], "=")
			out[k] = v
		}
		if call[i] == "--" {
			break
		}
	}
	return out
}

// flag returns the value after flag in an hc call, before "--".
func flag(call []string, name string) string {
	for i := 0; i+1 < len(call) && call[i] != "--"; i++ {
		if call[i] == name {
			return call[i+1]
		}
	}
	return ""
}

// program is the argv an hc run call starts.
func program(call []string) []string {
	i := slices.Index(call, "--")
	return call[i+1:]
}

func TestTerminalName(t *testing.T) {
	a := TerminalName("example.com/acme/shop", "add-login")
	if !regexp.MustCompile(`^tb-[0-9a-f]{12}$`).MatchString(a) {
		t.Errorf("name %q", a)
	}
	if b := TerminalName("example.com/acme/blog", "add-login"); a == b {
		t.Error("two repositories' add-login share a name")
	}
	if b := TerminalName("example.com/acme/shop", "add-search"); a == b {
		t.Error("two changes share a name")
	}
	if b := TerminalName("example.com/acme/shop", "add-login"); a != b {
		t.Error("the name is not stable")
	}
}

// Scenario: Agent tab in Herdr. add-login's build step starts in the
// terminal backend in repository shop: herdr-core has a live terminal with
// the change's name, in the worktree, labelled for space shop and tab
// "<change> · build"; it runs the agent under agent-hold.
func TestAgentTabInHerdr(t *testing.T) {
	f := newTermFixture(t)
	s := f.step("build", "builder", 1, interactiveFake())
	var gotName, gotAddr string
	var live *HCTerminal
	opts := termOpts
	opts.OnTerminal = func(name, addr string) {
		gotName, gotAddr = name, addr
		live = f.get(name)
	}
	o := f.run(s, opts)
	if o.Result == nil || o.Result.Status != Done || o.Source != messageLabel {
		t.Fatalf("outcome %+v", o)
	}
	runs := f.runCalls()
	if len(runs) != 1 {
		t.Fatalf("hc run calls %v", runs)
	}
	call := runs[0]
	if gotName != s.Term.Name || flag(call, "--name") != s.Term.Name || gotAddr == "" {
		t.Errorf("name %q address %q, call %v", gotName, gotAddr, call)
	}
	if !sameDir(flag(call, "--cwd"), s.Worktree) {
		t.Errorf("cwd %q, want the worktree %s", flag(call, "--cwd"), s.Worktree)
	}
	for _, fl := range []string{"--bg", "--no-parent", "--outlive-parent"} {
		if i := slices.Index(call, fl); i < 0 || i > slices.Index(call, "--") {
			t.Errorf("hc run lacks %s: %v", fl, call)
		}
	}
	want := map[string]string{
		"herdr.space": "shop", "herdr.tab": "fix-login · build",
		"tuckerbox.change": "fix-login", "tuckerbox.step": "build", "tuckerbox.round": "1", "tuckerbox.run": "run-1",
	}
	if got := labels(call); !mapsEqual(got, want) {
		t.Errorf("labels %v, want %v", got, want)
	}
	if !live.Live() || live.Labels["herdr.tab"] != "fix-login · build" {
		t.Errorf("terminal while running: %+v", live)
	}
	prog := program(call)
	if len(prog) < 4 || prog[0] != termTools.Tuckerbox || prog[1] != "agent-hold" || prog[2] != "--slot" || prog[3] != "0" {
		t.Errorf("terminal program %v, want tuckerbox agent-hold --slot 0 ...", prog)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// Scenario: Turn ends with a result. The build agent's turn ends with a
// valid result: it is accepted, the agent's program is ended after the
// result grace, and the terminal's history stays readable.
func TestTurnEndsWithResult(t *testing.T) {
	f := newTermFixture(t)
	s := f.step("build", "builder", 2, interactiveFake())
	opts := termOpts
	opts.ResultGrace = 700 * time.Millisecond
	start := time.Now()
	o := f.run(s, opts)
	took := time.Since(start)
	if o.Result == nil || o.Result.Round != 2 || o.Source != messageLabel {
		t.Fatalf("outcome %+v", o)
	}
	if _, err := ReadResult(o.ResultPath, Expect{Change: s.Change, Step: "build", Round: 2}); err != nil {
		t.Errorf("result file: %v", err)
	}
	if took < opts.ResultGrace {
		t.Errorf("the agent was ended %s after the start, before the result grace of %s", took, opts.ResultGrace)
	}
	term := f.get(s.Term.Name)
	if term.Live() {
		t.Error("the agent's program still runs after the step")
	}
	hist, err := f.hc.Read(context.Background(), s.Term.Name)
	if err != nil || !strings.Contains(string(hist), "fakeagent: fix-login build round 2") {
		t.Errorf("terminal history not readable after the step: %v\n%s", err, hist)
	}
	log := readFile(t, o.LogPath)
	for _, want := range []string{"started in terminal " + s.Term.Name, "accepted result from the final message", "history of terminal", "fakeagent: fix-login build round 2"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}

// Scenario: Stale result in a later turn. A turn ends with a result naming
// round 2 while the step is on round 3: it is ignored, and the next turn's
// result is accepted.
func TestStaleResultInLaterTurn(t *testing.T) {
	f := newTermFixture(t)
	f.env.SetPlan(t, map[string]testenv.Action{"build": {StaleFirst: true}})
	s := f.step("build", "builder", 3, interactiveFake())
	o := f.run(s, termOpts)
	if o.Result == nil || o.Result.Round != 3 {
		t.Fatalf("outcome %+v", o)
	}
	if len(o.Ignored) != 1 || !strings.Contains(o.Ignored[0], "round 2") {
		t.Errorf("ignored %q, want the round 2 result", o.Ignored)
	}
}

// waitTurn runs s in the background and waits until its agent waits for
// the person, while the step still runs.
func (f *termFixture) waitTurn(s Step) (<-chan Outcome, *[]string) {
	f.t.Helper()
	var mu sync.Mutex
	var states []string
	waiting := make(chan struct{})
	opts := termOpts
	opts.OnAgentState = func(st string) {
		mu.Lock()
		defer mu.Unlock()
		states = append(states, st)
		if st == AgentWaiting {
			close(waiting)
		}
	}
	done := make(chan Outcome, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		o, err := f.be.Run(ctx, s, opts)
		if err != nil {
			f.t.Error(err)
		}
		done <- o
	}()
	select {
	case <-waiting:
	case o := <-done:
		f.t.Fatalf("step ended before its agent waited: %+v", o)
	case <-time.After(30 * time.Second):
		f.t.Fatal("the agent never waited for the person")
	}
	return done, &states
}

// Scenario: Agent asks before finishing. The review agent ends a turn
// asking whether to also check the README, with no result: the step stays
// running, waiting for you, and the agent gets the person's answer in its
// tab.
func TestAgentAsksBeforeFinishing(t *testing.T) {
	f := newTermFixture(t)
	f.env.SetPlan(t, map[string]testenv.Action{"review": {Ask: "Should I also check the README?"}})
	s := f.step("review", "reviewer", 1, interactiveFake())
	done, states := f.waitTurn(s)
	time.Sleep(300 * time.Millisecond)
	select {
	case o := <-done:
		t.Fatalf("the step ended while the agent waited: %+v", o)
	default:
	}
	if !f.get(s.Term.Name).Live() {
		t.Fatal("the agent was ended for asking")
	}
	if err := f.hc.Send(context.Background(), s.Term.Name, "yes"); err != nil {
		t.Fatal(err)
	}
	o := <-done
	if o.Result == nil || o.Result.Summary != "answer: yes" {
		t.Fatalf("outcome %+v", o)
	}
	if got := *states; len(got) < 2 || got[0] != AgentWorking || got[1] != AgentWaiting {
		t.Errorf("agent states %v", got)
	}
}

// Scenario: Person answers a question. The build agent in its tab asks
// which of two designs to follow and the person types an answer there: the
// agent carries on with it in the same session.
func TestPersonAnswersAQuestion(t *testing.T) {
	f := newTermFixture(t)
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Ask: "Design A or design B?"}})
	s := f.step("build", "builder", 1, interactiveFake())
	done, _ := f.waitTurn(s)
	before := f.get(s.Term.Name)
	if err := f.hc.Send(context.Background(), s.Term.Name, "design B"); err != nil {
		t.Fatal(err)
	}
	o := <-done
	if o.Result == nil || o.Result.Summary != "answer: design B" {
		t.Fatalf("outcome %+v", o)
	}
	if n := len(f.runCalls()); n != 1 {
		t.Errorf("%d terminals started, want the same session throughout", n)
	}
	if after := f.get(s.Term.Name); after.Address != before.Address {
		t.Errorf("terminal %s became %s", before.Address, after.Address)
	}
}

// Scenario: Leftover terminal from an older round. A step starts round 4
// while the change's terminal from round 3 still lives: that terminal is
// ended first, then round 4's starts.
func TestLeftoverTerminalFromOlderRound(t *testing.T) {
	f := newTermFixture(t)
	s := f.step("build", "builder", 4, interactiveFake())
	old, err := f.hc.Start(context.Background(), HCStart{Name: s.Term.Name, Cwd: s.Worktree, Argv: []string{"sleep", "60"},
		Labels: [][2]string{{LabelChange, s.Change}, {LabelStep, "build"}, {LabelRound, "3"}}})
	if err != nil {
		t.Fatal(err)
	}
	// While it lives, herdr-core refuses a second terminal of the name.
	if _, err := f.hc.Start(context.Background(), HCStart{Name: s.Term.Name, Cwd: s.Worktree, Argv: []string{"true"}}); err == nil {
		t.Fatal("the fake hc started a second live terminal of one name")
	}
	o := f.run(s, termOpts)
	if o.Result == nil || o.Result.Round != 4 {
		t.Fatalf("outcome %+v", o)
	}
	var seq []string
	for _, c := range f.calls() {
		if c[0] == "kill" || c[0] == "run" {
			seq = append(seq, c[0])
		}
	}
	// run (the old one), run refused, kill (the old one), run (round 4),
	// kill (after the result).
	if want := []string{"run", "run", "kill", "run", "kill"}; !slices.Equal(seq, want) {
		t.Errorf("hc calls %v, want %v", seq, want)
	}
	runs := f.runCalls()
	if got := labels(runs[len(runs)-1])["tuckerbox.round"]; got != "4" {
		t.Errorf("new terminal's round %q", got)
	}
	if !strings.Contains(readFile(t, o.LogPath), "ending terminal "+s.Term.Name+" left from step build round 3") {
		t.Error("log does not say the old terminal was ended")
	}
	if term := f.get(s.Term.Name); term.Address == old.Address {
		t.Error("the round 3 terminal was kept")
	}
}

// A live terminal of this step and round is adopted, not replaced: its
// result is accepted and no second agent starts.
func TestAdoptLiveTerminal(t *testing.T) {
	f := newTermFixture(t)
	gate := filepath.Join(f.env.Home, "gate")
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: gate}})
	s := f.step("build", "builder", 2, interactiveFake())
	// The agent a run process started before it died: its brief written,
	// its terminal live and labelled for build round 2.
	if err := s.State.Ensure(); err != nil {
		t.Fatal(err)
	}
	pre := Outcome{BriefPath: s.State.Brief("build", 2), ResultPath: s.State.Result("build", 2), MessagePath: s.State.LastMessage("build", 2)}
	if err := f.be.prepare(s, &pre); err != nil {
		t.Fatal(err)
	}
	inner, err := s.Agent.ExpandInteractive(Values{Brief: "go", TurnHook: TurnHookArgv(termTools.Tuckerbox, s.State, "build", 2, HookArgv)})
	if err != nil {
		t.Fatal(err)
	}
	argv := HoldArgv(termTools.Tuckerbox, s.Term, stepEnv(s, pre.BriefPath, pre.ResultPath), inner)
	if _, err := f.hc.Start(context.Background(), HCStart{Name: s.Term.Name, Cwd: s.Worktree, Argv: argv,
		Labels: [][2]string{{LabelChange, s.Change}, {LabelStep, "build"}, {LabelRound, "2"}}}); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		testenv.WriteFile(t, gate, "go\n")
	}()
	o := f.run(s, termOpts)
	if o.Result == nil || o.Result.Round != 2 {
		t.Fatalf("outcome %+v", o)
	}
	if n := len(f.runCalls()); n != 1 {
		t.Errorf("%d terminals started, want the adopted one only", n)
	}
	if got := f.env.Starts(t); len(got) != 1 {
		t.Errorf("agent starts %v", got)
	}
	if !strings.Contains(readFile(t, o.LogPath), "adopted live terminal") {
		t.Error("log does not say the terminal was adopted")
	}
}

// A run that cannot look up the terminal it would adopt (herdr-core does
// not answer, even to a second try) cannot tell whether its agent lives:
// Run returns an *EndError naming the terminal, so the run keeps it for
// stop and resume, and the agent is not left unfindable.
func TestAdoptionLookupFails(t *testing.T) {
	f := newTermFixture(t)
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: filepath.Join(f.env.Home, "never")}})
	s := f.step("build", "builder", 2, interactiveFake())
	if err := s.State.Ensure(); err != nil {
		t.Fatal(err)
	}
	pre := Outcome{BriefPath: s.State.Brief("build", 2), ResultPath: s.State.Result("build", 2), MessagePath: s.State.LastMessage("build", 2)}
	if err := f.be.prepare(s, &pre); err != nil {
		t.Fatal(err)
	}
	inner, err := s.Agent.ExpandInteractive(Values{Brief: "go", TurnHook: TurnHookArgv(termTools.Tuckerbox, s.State, "build", 2, HookArgv)})
	if err != nil {
		t.Fatal(err)
	}
	argv := HoldArgv(termTools.Tuckerbox, s.Term, stepEnv(s, pre.BriefPath, pre.ResultPath), inner)
	if _, err := f.hc.Start(context.Background(), HCStart{Name: s.Term.Name, Cwd: s.Worktree, Argv: argv,
		Labels: [][2]string{{LabelChange, s.Change}, {LabelStep, "build"}, {LabelRound, "2"}}}); err != nil {
		t.Fatal(err)
	}

	f.setFault("get-fail", "0")
	o, err := f.be.Run(context.Background(), s, termOpts)
	var endErr *EndError
	if !errors.As(err, &endErr) || endErr.Name != s.Term.Name {
		t.Fatalf("Run with a failing lookup: %v (outcome %+v)", err, o)
	}
	if o.Stopped || o.Result != nil {
		t.Errorf("outcome %+v", o)
	}
	if n := len(f.runCalls()); n != 1 {
		t.Errorf("%d terminals started, want only the one to adopt", n)
	}
	f.setFault("get-fail", "")
	if !f.get(s.Term.Name).Live() {
		t.Fatal("the agent's terminal is not live")
	}
	// What stop does with the name the run kept: end the agent.
	if err := f.be.EndAgent(context.Background(), s.Term.Name); err != nil {
		t.Fatal(err)
	}
	if f.get(s.Term.Name).Live() {
		t.Error("the agent's terminal is live after it was ended")
	}
}

// Cancelling the run (stop) ends the agent's terminal.
func TestStopEndsTerminal(t *testing.T) {
	f := newTermFixture(t)
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: filepath.Join(f.env.Home, "never")}})
	s := f.step("build", "builder", 1, interactiveFake())
	ctx, cancel := context.WithCancel(context.Background())
	opts := termOpts
	opts.OnTerminal = func(string, string) {
		go func() {
			time.Sleep(200 * time.Millisecond)
			cancel()
		}()
	}
	o, err := f.be.Run(ctx, s, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !o.Stopped || o.Result != nil {
		t.Errorf("outcome %+v", o)
	}
	if f.get(s.Term.Name).Live() {
		t.Error("terminal still live after stop")
	}
}

// A terminal that ends without a valid result fails the step, as an agent
// that exits does.
func TestTerminalEndsWithoutResult(t *testing.T) {
	f := newTermFixture(t)
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Status: "none", ExitAfter: true, Exit: 4}})
	s := f.step("build", "builder", 1, interactiveFake())
	o := f.run(s, termOpts)
	if o.Result != nil || o.Status() != Failed || o.ExitCode != 4 {
		t.Errorf("outcome %+v", o)
	}
	if !strings.Contains(o.FailureReason(), "exited with status 4") {
		t.Errorf("reason %q", o.FailureReason())
	}
}

// Interactive start: the built-in claude and codex agents run their
// interactive commands with the prompt, model, effort and profile flags,
// the TUCKERBOX_* environment and their own turn hooks, passed with
// --settings and -c notify=.
func TestBuiltinInteractiveStart(t *testing.T) {
	for _, name := range []string{"claude", "codex"} {
		t.Run(name, func(t *testing.T) {
			f := newTermFixture(t)
			tmpl := Builtin()[name]
			tmpl.Interactive = slices.Clone(tmpl.Interactive)
			tmpl.Interactive[0] = termTools.FakeAgent
			s := f.step("build", "builder", 1, tmpl)
			s.Model, s.Effort, s.Permissions = "big-model", "high", []string{"--profile-flag"}
			o := f.run(s, termOpts)
			// The fake agent builds its result from TUCKERBOX_*: accepted
			// means the environment reached it, and the hook worked.
			if o.Result == nil || o.Source != messageLabel {
				t.Fatalf("outcome %+v\n%s", o, readFile(t, o.LogPath))
			}
			prog := program(f.runCalls()[0])
			inner := prog[slices.Index(prog, "--")+1:]
			hold := prog[:slices.Index(prog, "--")]
			for _, env := range []string{"TUCKERBOX_CHANGE=fix-login", "TUCKERBOX_STEP=build", "TUCKERBOX_ROUND=1",
				"TUCKERBOX_BRIEF=" + o.BriefPath, "TUCKERBOX_RESULT=" + o.ResultPath} {
				if !slices.Contains(hold, env) {
					t.Errorf("agent-hold lacks --env %s: %v", env, hold)
				}
			}
			joined := strings.Join(inner, "\x00")
			model, effort := "--model\x00big-model\x00--effort\x00high", "\x00--settings\x00"
			if name == "codex" {
				model, effort = "-m\x00big-model\x00-c\x00model_reasoning_effort=high", "\x00-c\x00notify=["
			}
			for _, want := range []string{model, "\x00--profile-flag\x00", effort} {
				if !strings.Contains(joined, want) {
					t.Errorf("command lacks %q: %q", want, inner)
				}
			}
			prompt := inner[len(inner)-1]
			if !strings.Contains(prompt, "Read the Tuckerbox brief at "+o.BriefPath) {
				t.Errorf("prompt %q", prompt)
			}
			if strings.Contains(joined, "-p\x00") || strings.Contains(joined, "exec") {
				t.Errorf("not the interactive command: %q", inner)
			}
		})
	}
}

// agent-turn writes each CLI's final message to the step's last-message
// file.
func TestAgentTurnCommand(t *testing.T) {
	if termTools == nil {
		t.Skip("no tuckerbox build on Windows")
	}
	s := newStep(t, "build", "builder", 1, Template{})
	msgPath := s.State.LastMessage("build", 1)
	tr := filepath.Join(t.TempDir(), "t.jsonl")
	testenv.WriteFile(t, tr, `{"type":"assistant","message":{"id":"a","content":[{"type":"text","text":"from claude"}]}}`+"\n")
	stop, err := json.Marshal(map[string]string{"transcript_path": tr})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		style HookStyle
		args  []string
		stdin string
		want  string
	}{
		{HookClaude, nil, string(stop), "from claude"},
		{HookCodex, []string{`{"type":"agent-turn-complete","last-assistant-message":"from codex"}`}, "", "from codex"},
		{HookArgv, []string{"plain words"}, "", "plain words"},
	}
	for _, c := range cases {
		argv := append(TurnHookArgv(termTools.Tuckerbox, s.State, "build", 1, c.style), c.args...)
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdin = strings.NewReader(c.stdin)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", argv, err, out)
		}
		if got := readFile(t, msgPath); got != c.want {
			t.Errorf("style %d: message %q, want %q", c.style, got, c.want)
		}
	}
}

// setFault sets a fake hc fault file (see testdata/fakehc); "" clears it.
func (f *termFixture) setFault(name, value string) {
	f.t.Helper()
	p := filepath.Join(f.env.HCDir, name)
	if value == "" {
		_ = os.Remove(p)
		return
	}
	testenv.WriteFile(f.t, p, value)
}

// A stop whose hc kill fails, or whose terminal does not go within the end
// timeout, is not a stop: Run returns an *EndError, so the run keeps the
// terminal's name and slot and does not record stopped while the agent
// may live.
func TestStopWhenTheTerminalWontEnd(t *testing.T) {
	for _, mode := range []string{"fail", "ignore"} {
		t.Run(mode, func(t *testing.T) {
			f := newTermFixture(t)
			f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: filepath.Join(f.env.Home, "never")}})
			s := f.step("build", "builder", 1, interactiveFake())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opts := termOpts
			opts.EndTimeout = 300 * time.Millisecond
			opts.OnTerminal = func(_, addr string) {
				if addr == "" {
					return
				}
				go func() {
					time.Sleep(200 * time.Millisecond)
					f.setFault("kill-mode", mode)
					cancel()
				}()
			}
			start := time.Now()
			_, err := f.be.Run(ctx, s, opts)
			var ee *EndError
			if !errors.As(err, &ee) || ee.Name != s.Term.Name {
				t.Fatalf("Run returned %v, want an *EndError for %s", err, s.Term.Name)
			}
			if time.Since(start) > 20*time.Second {
				t.Errorf("Run took %s to give up", time.Since(start))
			}
			if !f.get(s.Term.Name).Live() {
				t.Fatal("the fault did not keep the terminal live")
			}
			log := readFile(t, s.State.Log("build"))
			if !strings.Contains(log, "may still be live") || strings.Contains(log, "finished: exit") {
				t.Errorf("log:\n%s", log)
			}
			f.setFault("kill-mode", "")
			if err := f.hc.End(context.Background(), s.Term.Name, 10*time.Second); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// A result was accepted but the terminal would not end after the grace:
// the step is not finished, so the workflow cannot move on beside a live
// agent.
func TestResultButTheTerminalWontEnd(t *testing.T) {
	f := newTermFixture(t)
	f.env.SetPlan(t, map[string]testenv.Action{"build": {}}) // reports and stays
	f.setFault("kill-mode", "fail")
	defer f.setFault("kill-mode", "")
	s := f.step("build", "builder", 1, interactiveFake())
	opts := termOpts
	opts.EndTimeout = 300 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	o, err := f.be.Run(ctx, s, opts)
	var ee *EndError
	if !errors.As(err, &ee) {
		t.Fatalf("Run returned %v, want an *EndError", err)
	}
	if o.Result == nil {
		t.Error("the result was not accepted before the terminal was ended")
	}
}

// Launch intent: the run's state names the terminal before hc run starts
// it, so a run process that dies while hc starts it leaves a terminal it
// can find.
func TestTerminalNamedBeforeStart(t *testing.T) {
	f := newTermFixture(t)
	f.env.SetPlan(t, map[string]testenv.Action{"build": {ExitAfter: true}})
	s := f.step("build", "builder", 1, interactiveFake())
	type call struct {
		name, addr string
		runs       int
	}
	var got []call
	opts := termOpts
	opts.OnTerminal = func(name, addr string) { got = append(got, call{name, addr, len(f.runCalls())}) }
	f.run(s, opts)
	if len(got) != 2 || got[0] != (call{s.Term.Name, "", 0}) || got[1].addr == "" || got[1].runs != 1 {
		t.Errorf("OnTerminal calls %+v, want the name before hc run, then the address", got)
	}
}

// Stopped after herdr-core launched the terminal but before hc run
// answered: the error from hc run does not mean no agent started. Run
// finds the terminal with its own context and ends it before it reports
// stopped; when it will not end, Run gives an *EndError so the run keeps
// what finds the agent again.
func TestStopWhileTheTerminalStarts(t *testing.T) {
	for _, mode := range []string{"", "fail"} {
		t.Run("kill="+mode, func(t *testing.T) {
			f := newTermFixture(t)
			f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: filepath.Join(f.env.Home, "never")}})
			f.setFault("run-delay", "3s")
			s := f.step("build", "builder", 1, interactiveFake())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opts := termOpts
			opts.EndTimeout = 2 * time.Second
			opts.OnTerminal = func(_, addr string) {
				if addr != "" {
					return
				}
				go func() {
					// Cancel once hc run has launched the terminal and
					// sleeps before its answer.
					for f.get(s.Term.Name) == nil {
						time.Sleep(20 * time.Millisecond)
					}
					f.setFault("kill-mode", mode)
					cancel()
				}()
			}
			o, err := f.be.Run(ctx, s, opts)
			defer func() {
				f.setFault("kill-mode", "")
				_ = f.hc.End(context.Background(), s.Term.Name, 10*time.Second)
			}()
			if mode == "fail" {
				var ee *EndError
				if !errors.As(err, &ee) || ee.Name != s.Term.Name {
					t.Fatalf("Run returned %v, want an *EndError for %s", err, s.Term.Name)
				}
				if !f.get(s.Term.Name).Live() {
					t.Fatal("the fault did not keep the terminal live")
				}
				return
			}
			if err != nil || !o.Stopped {
				t.Fatalf("Run returned stopped=%v, %v; want stopped", o.Stopped, err)
			}
			if term := f.get(s.Term.Name); term == nil || term.Live() {
				t.Fatalf("terminal after stop: %+v; want it started and ended", term)
			}
		})
	}
}

// hc run launched the terminal but its answer could not be read: Run finds
// the terminal and watches it as if hc run had answered.
func TestStartWithAnUnreadableAnswer(t *testing.T) {
	f := newTermFixture(t)
	f.env.SetPlan(t, map[string]testenv.Action{"build": {ExitAfter: true}})
	f.setFault("run-reply", "garbage")
	s := f.step("build", "builder", 1, interactiveFake())
	o := f.run(s, termOpts)
	if o.Result == nil || o.Result.Status != Done {
		t.Fatalf("outcome %+v; want the agent's result", o)
	}
	if len(f.runCalls()) != 1 {
		t.Errorf("hc run calls: %d, want 1", len(f.runCalls()))
	}
}
