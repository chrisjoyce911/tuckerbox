package cli

import (
	"bufio"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

const (
	boardSafety = "fix-board-safety"
	question    = "Live Herdr check needed; keep or drop install rollback?"
	decision    = "Herdr check done by hand: drawer works. Keep rollback."
)

// blockedBuild runs fix-board-safety until its build step blocks with the
// question of the scenarios.
func blockedBuild(t *testing.T) *cliFixture {
	t.Helper()
	f := newCLI(t, testenv.RepoOptions{Changes: []string{boardSafety}})
	f.env.SetPlan(t, map[string]testenv.Action{
		boardSafety + ":build:1": {Status: "blocked", Summary: question, Findings: []testenv.Finding{
			{Severity: "info", File: "install.go", Detail: "Rollback restores the previous mod when install fails."},
		}},
	})
	if res := f.tb("run", "--foreground", boardSafety); res.code != 0 {
		t.Fatalf("run: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	if r := f.runOf(boardSafety); r.State != runs.Blocked || r.Step != "build" {
		t.Fatalf("run is %s at %s", r.State, r.Step)
	}
	return f
}

func (f *cliFixture) events(id string) []history.Event {
	f.t.Helper()
	events, err := history.Read(history.File(mustState(f.t, f.worktreeOf(id))))
	if err != nil {
		f.t.Fatal(err)
	}
	return events
}

func answeredEvents(events []history.Event) []history.Event {
	var out []history.Event
	for _, e := range events {
		if e.Kind == history.Answered {
			out = append(out, e)
		}
	}
	return out
}

// Scenario: Builder blocked on a decision. Status (table and JSON) and the
// board's card show the blocked result's summary as the question, with
// step build and the round.
func TestBuilderBlockedOnADecision(t *testing.T) {
	f := blockedBuild(t)

	res := f.tb("status", "--json")
	var doc struct {
		Runs []struct {
			Change   string
			Question *runs.QuestionView
		}
	}
	if err := json.Unmarshal([]byte(res.stdout), &doc); err != nil {
		t.Fatal(err)
	}
	q := doc.Runs[0].Question
	if q == nil || q.Kind != "blocked" || q.Summary != question || q.Step != "build" || q.Round != 1 ||
		len(q.Findings) != 1 || q.Findings[0].File != "install.go" {
		t.Fatalf("status --json question: %+v\n%s", q, res.stdout)
	}
	res = f.tb("status")
	for _, want := range []string{"fix-board-safety asks (step build, round 1):", "  " + question, "[info] install.go: Rollback restores", "tuckerbox answer fix-board-safety <text>"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("status lacks %q:\n%s", want, res.stdout)
		}
	}

	res = f.tb("board", "--json")
	var board struct {
		Cards []struct {
			Change   string
			State    string
			Question *runs.QuestionView
		}
	}
	if err := json.Unmarshal([]byte(res.stdout), &board); err != nil {
		t.Fatalf("board: %v\n%s%s", err, res.stdout, res.stderr)
	}
	c := board.Cards[0]
	if c.State != "blocked" || c.Question == nil || c.Question.Summary != question || c.Question.Step != "build" || c.Question.Round != 1 {
		t.Fatalf("card: %+v %+v", c, c.Question)
	}
	// Card detail reads the same card.
	res = f.tb("board", "--json", "--change", boardSafety)
	if !strings.Contains(res.stdout, `"question"`) || !strings.Contains(res.stdout, question) {
		t.Errorf("card detail lacks the question:\n%s", res.stdout)
	}
}

// Scenario: Answer from the command line. Run state holds the answer with
// the owner's name, the time and step build; history has an answered
// event; the run stays blocked. The board shows the answer under the
// question.
func TestAnswerFromTheCommandLine(t *testing.T) {
	f := blockedBuild(t)
	before := f.runOf(boardSafety).Since

	res := f.tb("answer", boardSafety, decision)
	if res.code != 0 || !strings.Contains(res.stdout, "recorded your answer to step build round 1") || !strings.Contains(res.stdout, "stays blocked") {
		t.Fatalf("answer: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	r := f.runOf(boardSafety)
	if r.State != runs.Blocked || !r.Since.Equal(before) {
		t.Errorf("run is %s since %v (was since %v)", r.State, r.Since, before)
	}
	if len(r.Answers) != 1 {
		t.Fatalf("answers %+v", r.Answers)
	}
	a := r.Answers[0]
	if a.Text != decision || a.By != "Test" || a.Step != "build" || a.Round != 1 || a.At.IsZero() || a.Used {
		t.Errorf("answer %+v", a)
	}
	ev := answeredEvents(f.events(boardSafety))
	if len(ev) != 1 || ev[0].Text != decision || ev[0].By != "Test" || ev[0].Step != "build" || ev[0].Round != 1 || ev[0].Terminal != "" {
		t.Errorf("answered events %+v", ev)
	}
	// From standard input and from a file too; status counts them.
	cmd := f.tbCmd("answer", boardSafety, "-")
	cmd.Stdin = strings.NewReader("line one\nline two\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("answer -: %v\n%s", err, out)
	}
	p := filepath.Join(t.TempDir(), "answer.txt")
	testenv.WriteFile(t, p, "from a file\n")
	if res := f.tb("answer", boardSafety, "--file", p); res.code != 0 {
		t.Fatalf("answer --file: %d %s", res.code, res.stderr)
	}
	r = f.runOf(boardSafety)
	if len(r.Answers) != 3 || r.Answers[1].Text != "line one\nline two" || r.Answers[2].Text != "from a file" {
		t.Errorf("answers %+v", r.Answers)
	}
	if res := f.tb("status"); !strings.Contains(res.stdout, "answered 3 time(s)") {
		t.Errorf("status:\n%s", res.stdout)
	}
	res = f.tb("board", "--json")
	if !strings.Contains(res.stdout, `"text": "from a file"`) {
		t.Errorf("board lacks the answers:\n%s", res.stdout)
	}

	// Over 16 KiB, and empty: refused, nothing recorded.
	cmd = f.tbCmd("answer", boardSafety, "-")
	cmd.Stdin = strings.NewReader(strings.Repeat("x", runs.MaxAnswer+1))
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "the limit is 16384 (16 KiB)") {
		t.Errorf("long answer: %v\n%s", err, out)
	}
	if res := f.tb("answer", boardSafety, "  "); res.code != 1 || !strings.Contains(res.stderr, "empty") {
		t.Errorf("empty answer: %d %s", res.code, res.stderr)
	}
	if n := len(f.runOf(boardSafety).Answers); n != 3 {
		t.Errorf("%d answers after refusals", n)
	}
	if res := f.tb("answer", boardSafety); res.code != 2 {
		t.Errorf("no text: %d %s", res.code, res.stderr)
	}
}

// Scenario: Run not blocked. Answering a run that is running headless
// exits non-zero, says it isn't blocked and records nothing.
func TestAnswerRunNotBlocked(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: f.gate("build")}})
	if res := f.tb("run", "add-login"); res.code != 0 {
		t.Fatal(res.stderr)
	}
	f.waitState("add-login", "running", "build")
	before := len(f.events("add-login"))
	res := f.tb("answer", "add-login", "use plan B")
	if res.code != 1 || !strings.Contains(res.stderr, "isn't blocked (it is running)") {
		t.Errorf("answer: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	if r := f.runOf("add-login"); len(r.Answers) != 0 {
		t.Errorf("answers %+v", r.Answers)
	}
	if _, err := os.Stat(runs.AnswerInbox(mustState(t, f.worktreeOf("add-login")))); err == nil {
		t.Error("an answer was left in the inbox")
	}
	f.open("build")
	f.waitState("add-login", "ready", "")
	// A ready run isn't blocked either.
	res = f.tb("answer", "add-login", "use plan B")
	if res.code != 1 || !strings.Contains(res.stderr, "isn't blocked (it is ready)") {
		t.Errorf("answer to ready: exit %d %s", res.code, res.stderr)
	}
	if got := answeredEvents(f.events("add-login")); len(got) != 0 || len(f.events("add-login")) <= before {
		t.Errorf("answered events %+v", got)
	}
}

// normaliseTimes replaces the answers' times in a brief, which are when
// the test ran.
var answerTime = regexp.MustCompile(`at \d{4}-\d{2}-\d{2} \d{2}:\d{2} AEST`)

// Scenarios: Resume with the decision, and Two answers. The build step's
// new brief holds the question and both answers, oldest first (a
// snapshot); the agent's result uses them, and the review's brief and any
// later build brief leave them out.
func TestResumeWithTheDecision(t *testing.T) {
	f := blockedBuild(t)
	if res := f.tb("answer", boardSafety, decision); res.code != 0 {
		t.Fatal(res.stderr)
	}
	if res := f.tb("answer", boardSafety, "Also: keep the backup for one release."); res.code != 0 {
		t.Fatal(res.stderr)
	}
	if res := f.tb("resume", "--foreground", boardSafety); res.code != 0 {
		t.Fatalf("resume: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	st := mustState(t, f.worktreeOf(boardSafety))
	data, err := os.ReadFile(st.Brief("build", 2))
	if err != nil {
		t.Fatal(err)
	}
	brief := string(data)
	start := strings.Index(brief, "## Answers from a person")
	end := strings.Index(brief, "## Result")
	if start < 0 || end < start {
		t.Fatalf("build round 2 brief has no answers section:\n%s", brief)
	}
	got := answerTime.ReplaceAllString(brief[start:end], "at <time> AEST")
	golden := filepath.Join("testdata", "brief-answers.md")
	if *updateGolden {
		testenv.WriteFile(t, golden, got)
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("answers section differs from %s:\n--- got\n%s--- want\n%s", golden, got, want)
	}
	if strings.Index(got, decision) > strings.Index(got, "keep the backup") {
		t.Error("answers not oldest first")
	}
	r := f.runOf(boardSafety)
	if r.State != runs.Ready {
		t.Fatalf("run is %s: %s", r.State, r.Reason)
	}
	for _, a := range r.Answers {
		if !a.Used {
			t.Errorf("answer not used after the build reported: %+v", a)
		}
	}
	data, _ = os.ReadFile(st.Brief("review", 2))
	if strings.Contains(string(data), "Answers from a person") {
		t.Error("the review's brief holds the build's answers")
	}
	// A later build leaves the used answers out.
	if res := f.tb("rerun", "--foreground", boardSafety, "build"); res.code != 0 {
		t.Fatal(res.stderr)
	}
	data, _ = os.ReadFile(st.Brief("build", 3))
	if strings.Contains(string(data), "Answers from a person") {
		t.Error("a later build brief holds used answers")
	}
}

// The answers stay in the step's briefs until it reports a result: a
// resume whose build is stopped before reporting gives the next build
// brief the answers again.
func TestAnswersStayUntilAResult(t *testing.T) {
	f := blockedBuild(t)
	if res := f.tb("answer", boardSafety, decision); res.code != 0 {
		t.Fatal(res.stderr)
	}
	f.env.SetPlan(t, map[string]testenv.Action{boardSafety + ":build:2": {WaitFor: f.gate("never")}})
	if res := f.tb("resume", boardSafety); res.code != 0 {
		t.Fatal(res.stderr)
	}
	f.waitState(boardSafety, "running", "build")
	if res := f.tb("stop", boardSafety); res.code != 0 {
		t.Fatal(res.stderr)
	}
	r := f.runOf(boardSafety)
	if len(r.Answers) != 1 || r.Answers[0].Used {
		t.Fatalf("answers after a stop: %+v", r.Answers)
	}
	if res := f.tb("resume", "--foreground", boardSafety); res.code != 0 {
		t.Fatal(res.stderr)
	}
	st := mustState(t, f.worktreeOf(boardSafety))
	for _, round := range []int{2, 3} {
		data, _ := os.ReadFile(st.Brief("build", round))
		if !strings.Contains(string(data), decision) {
			t.Errorf("build round %d brief lacks the answer", round)
		}
	}
	if r := f.runOf(boardSafety); !r.Answers[0].Used {
		t.Errorf("answer not used after the build reported")
	}
}

// Scenario: Resume refused after the answer. The resume's own gate
// refuses (here: this machine says agent_backend = "terminal" and
// herdr-core isn't running, which refuses every run and resume): the
// answer is recorded, the output says the resume was refused and why,
// and the run stays blocked; a later resume uses the answer.
func TestResumeRefusedAfterTheAnswer(t *testing.T) {
	f := blockedBuild(t)
	machineConfig(t, f.env.Home, "agent_backend = \"terminal\"\n")
	t.Setenv("FAKEHC_SERVER", "down")
	res := f.tb("answer", boardSafety, "--resume", "use plan B")
	if res.code != 1 {
		t.Fatalf("answer --resume: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "recorded your answer") || !strings.Contains(res.stderr, "herdr-core isn't running") ||
		!strings.Contains(res.stderr, "the answer is recorded, but the run was not resumed") {
		t.Errorf("output:\n%s%s", res.stdout, res.stderr)
	}
	r := f.runOf(boardSafety)
	if r.State != runs.Blocked || len(r.Answers) != 1 || r.Answers[0].Text != "use plan B" {
		t.Errorf("run %s, answers %+v", r.State, r.Answers)
	}
	if got := answeredEvents(f.events(boardSafety)); len(got) != 1 {
		t.Errorf("answered events %+v", got)
	}
	// With the gate passed, answer --resume resumes, and the build's
	// brief holds both answers.
	machineConfig(t, f.env.Home, "")
	res = f.tb("answer", boardSafety, "--resume", "and keep rollback")
	if res.code != 0 || !strings.Contains(res.stdout, "started") {
		t.Fatalf("answer --resume: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	f.waitState(boardSafety, "ready", "")
	data, _ := os.ReadFile(mustState(t, f.worktreeOf(boardSafety)).Brief("build", 2))
	if !strings.Contains(string(data), "use plan B") || !strings.Contains(string(data), "and keep rollback") {
		t.Errorf("build brief lacks the answers:\n%s", data)
	}
}

// Scenario: Answer without opening the tab. The review agent waits in its
// terminal; answer types the text into it (the fake hc records the send),
// history gets an answered event naming the terminal, and the run keeps
// running: the agent goes on with the answer.
func TestAnswerWithoutOpeningTheTab(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	const reply = "yes, check the README too"
	f.plan(map[string]testenv.Action{
		"review": {Ask: "Should I check the README as well?", ExitAfter: true},
	})
	f.start("add-login")
	f.waitAgent("add-login", "review")
	waitFor(t, "the review agent's question", func() bool { return f.runOf("add-login").AgentState == agent.AgentWaiting })
	name, addr := f.runOf("add-login").Terminal.Name, f.runOf("add-login").Terminal.Address

	// Status and the board show what it asks.
	res := f.tb("status")
	if !strings.Contains(res.stdout, "add-login waits for you (step review, round 1):") || !strings.Contains(res.stdout, "Should I check the README as well?") {
		t.Errorf("status:\n%s", res.stdout)
	}
	res = f.tb("board", "--json")
	if !strings.Contains(res.stdout, `"kind": "waiting"`) {
		t.Errorf("board lacks the waiting question:\n%s", res.stdout)
	}
	if res := f.tb("answer", "add-login", "--resume", reply); res.code != 1 || !strings.Contains(res.stderr, "answer without --resume") {
		t.Errorf("answer --resume to a running run: %d %s", res.code, res.stderr)
	}

	res = f.tb("answer", "add-login", reply)
	if res.code != 0 || !strings.Contains(res.stdout, "typed your answer into the review agent's terminal") {
		t.Fatalf("answer: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	if !f.sent(addr, reply) {
		t.Errorf("hc send %s %q not recorded", addr, reply)
	}
	// The run goes on (the agent reports its result with the answer),
	// never blocked, and run state holds no answer for a later brief.
	if r := f.runOf("add-login"); r.State == runs.Blocked || len(r.Answers) != 0 {
		t.Errorf("run %s, answers %+v", r.State, r.Answers)
	}
	waitFor(t, "the answered event", func() bool { return len(answeredEvents(f.events("add-login"))) == 1 })
	ev := answeredEvents(f.events("add-login"))[0]
	if ev.Text != reply || ev.By != "Test" || ev.Step != "review" || ev.Round != 1 || ev.Terminal != name {
		t.Errorf("answered %+v", ev)
	}
	f.waitState("add-login", "ready", "")
	r := f.runOf("add-login")
	if got := r.History[len(r.History)-1].Summary; got != "answer: "+reply {
		t.Errorf("review summary %q", got)
	}
	if got := answeredEvents(f.events("add-login")); len(got) != 1 {
		t.Errorf("answered events %d", len(got))
	}
}

// An orphaned agent (its run process died) that waits is answered with no
// run process: answer takes the change lock and records the event itself.
func TestAnswerAnOrphanedWaitingAgent(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.plan(map[string]testenv.Action{"build": {Ask: "Which design?", ExitAfter: true}})
	f.start("add-login")
	f.waitAgent("add-login", "build")
	waitFor(t, "the build agent's question", func() bool { return f.runOf("add-login").AgentState == agent.AgentWaiting })
	f.kill("add-login")
	name, addr := f.runOf("add-login").Terminal.Name, f.runOf("add-login").Terminal.Address

	res := f.tb("answer", "add-login", "the second one")
	if res.code != 0 || !strings.Contains(res.stdout, "typed your answer") {
		t.Fatalf("answer: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	if !f.sent(addr, "the second one") {
		t.Error("send not recorded")
	}
	if ev := answeredEvents(f.events("add-login")); len(ev) != 1 || ev[0].Terminal != name {
		t.Errorf("answered events %+v", ev)
	}
	// The agent went on with the answer; stop settles the orphaned run.
	f.tb("stop", "add-login")
}

// tbCmd is the tuckerbox command tb runs, for a test that sets its input.
func (f *cliFixture) tbCmd(args ...string) *exec.Cmd {
	cmd := exec.Command(tools.Tuckerbox, args...)
	cmd.Dir = f.repo
	return cmd
}

// sent reports whether the fake hc was asked to send text to terminal
// target (a name or an address).
func (f *cliFixture) sent(target, text string) bool {
	file, err := os.Open(filepath.Join(f.env.HCDir, "calls.log"))
	if err != nil {
		return false
	}
	defer file.Close()
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		var args []string
		if json.Unmarshal(sc.Bytes(), &args) != nil || len(args) == 0 || args[0] != "send" {
			continue
		}
		if args[len(args)-2] == target && args[len(args)-1] == text {
			return true
		}
	}
	return false
}
