package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// report is what the fake agent's report mode says it got: its arguments,
// long ones as their length and hash, and its standard input.
type report struct {
	Args  []string `json:"args"`
	Stdin string   `json:"stdin"`
}

func digest(s string) string {
	if len(s) <= 200 {
		return s
	}
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%d bytes sha256:%s", len(s), hex.EncodeToString(sum[:]))
}

func reportOf(t *testing.T, o Outcome) report {
	t.Helper()
	if o.Status() != Done {
		t.Fatalf("status %s: %s\n%s", o.Status(), o.FailureReason(), o.Tail)
	}
	var r report
	if err := json.Unmarshal([]byte(o.Result.Summary), &r); err != nil {
		t.Fatalf("summary %q: %v", o.Result.Summary, err)
	}
	return r
}

// builtinAsFake is built-in agent name with the fake agent as its program,
// so the step runs the built-in's real arguments.
func builtinAsFake(name string) Template {
	tpl := Builtin()[name]
	tpl.Command = append([]string{fakeAgent}, tpl.Command[1:]...)
	return tpl
}

// longStep is a step whose brief is about size characters, padded with
// an earlier round's finding.
func longStep(t *testing.T, agent Template, size int) Step {
	t.Helper()
	s := newStep(t, "build", "builder", 2, agent)
	s.Earlier = []Result{{Change: "fix-login", Step: "review", Round: 1, Status: Rejected, Summary: "too long",
		Findings: []Finding{{Severity: "low", File: "a.go", Detail: strings.Repeat("word ", size/5)}}}}
	return s
}

// Scenario: Custom agent that wants a file. Agent omp has command
// ["omp", "--prompt-file", "{brief_file}"]: Tuckerbox passes the brief file's
// path, and the brief's text nowhere in the command.
func TestCustomAgentWantsFile(t *testing.T) {
	s := newStep(t, "build", "builder", 1, fake("report", "--prompt-file", PhBriefFile))
	o := run(t, s, fast)
	r := reportOf(t, o)
	if want := []string{"--prompt-file", o.BriefPath}; strings.Join(r.Args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("args %q, want %q", r.Args, want)
	}
	brief := readFile(t, o.BriefPath)
	for _, a := range append(append([]string{}, r.Args...), o.Argv...) {
		if strings.Contains(a, "Tuckerbox brief") || a == brief {
			t.Errorf("brief text in the command: %q", a)
		}
	}
	if r.Stdin != "" {
		t.Errorf("stdin %q, want empty", r.Stdin)
	}
}

// Scenario: Agent reads no file. A step starts a fake agent that may not
// read the state folder, with {brief} in its command: the agent receives the
// whole brief text as that argument, and the brief file is also written.
// The fake agent's report mode opens no file and reports on stdout.
func TestAgentReadsNoFile(t *testing.T) {
	s := newStep(t, "build", "builder", 1, fake("report", PhBrief))
	o := run(t, s, fast)
	r := reportOf(t, o)
	brief := readFile(t, o.BriefPath)
	if !strings.HasPrefix(brief, "# Tuckerbox brief: step build, round 1, change fix-login") {
		t.Fatalf("brief file:\n%s", brief)
	}
	if len(r.Args) != 1 || r.Args[0] != digest(brief) {
		t.Errorf("agent got %q, want the whole brief (%s)", r.Args, digest(brief))
	}
	if o.Source != messageLabel {
		t.Errorf("result from %s, want the final message", o.Source)
	}
}

// Scenario: Build after rejection, with the brief inline: the build agent's
// argument holds round 1's findings.
func TestBuildAfterRejectionInline(t *testing.T) {
	s := newStep(t, "build", "builder", 2, fake("argv", PhBrief))
	s.Earlier = []Result{{Change: "fix-login", Step: "review", Round: 1, Status: Rejected,
		Findings: []Finding{{Severity: "high", File: "login.go", Detail: "password is logged"}}}}
	o := run(t, s, fast)
	if o.Status() != Done {
		t.Fatalf("status %s: %s", o.Status(), o.FailureReason())
	}
	var got struct {
		Args []string `json:"args"`
	}
	if err := json.Unmarshal([]byte(o.Result.Summary), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Args) != 1 || !strings.Contains(got.Args[0], "[high] `login.go`: password is logged") {
		t.Errorf("agent's brief lacks round 1's findings: %q", got.Args)
	}
}

// Scenario: Long brief to Claude. The brief makes the command line longer
// than 24,000 characters: the command has -p with no brief argument, and the
// agent reads the whole brief on standard input.
func TestLongBriefToClaude(t *testing.T) {
	s := longStep(t, builtinAsFake("claude"), 30000)
	s.Env = []string{"FAKEAGENT_MODE=report"}
	o := run(t, s, fast)
	r := reportOf(t, o)
	brief := readFile(t, o.BriefPath)
	if want := []string{"-p", "--output-format", "stream-json", "--verbose"}; strings.Join(r.Args, " ") != strings.Join(want, " ") {
		t.Errorf("args %q, want %q", r.Args, want)
	}
	if r.Stdin != digest(brief) {
		t.Errorf("stdin %s, want the whole brief %s", r.Stdin, digest(brief))
	}
	if !strings.Contains(readFile(t, o.LogPath), "sending it on standard input") {
		t.Error("log does not say the brief went on standard input")
	}
}

// Scenario: Long brief to Codex. The command ends with - in place of the
// brief, and the agent reads the whole brief on standard input.
func TestLongBriefToCodex(t *testing.T) {
	s := longStep(t, builtinAsFake("codex"), 30000)
	s.Env = []string{"FAKEAGENT_MODE=report"}
	o := run(t, s, fast)
	r := reportOf(t, o)
	if n := len(r.Args); n < 2 || r.Args[n-2] != "--" || r.Args[n-1] != "-" {
		t.Errorf("args %q, want them to end with -- -", r.Args)
	}
	if brief := readFile(t, o.BriefPath); r.Stdin != digest(brief) {
		t.Errorf("stdin %s, want the whole brief %s", r.Stdin, digest(brief))
	}
}

// Scenario: Long brief to a custom agent. A custom agent passes {brief} and
// its brief makes the command line too long: the argument holds an
// instruction naming the brief file, and the step log says the brief was
// too long to pass inline.
func TestLongBriefToCustomAgent(t *testing.T) {
	s := longStep(t, fake("report", "--prompt", PhBrief), 30000)
	o := run(t, s, fast)
	r := reportOf(t, o)
	instr := readBriefAt(o.BriefPath)
	if len(r.Args) != 2 || r.Args[1] != digest(instr) {
		t.Errorf("args %q, want the instruction %q", r.Args, instr)
	}
	if !strings.Contains(instr, o.BriefPath) || !strings.Contains(instr, "blocked") {
		t.Errorf("instruction %q should name the brief file and say to report blocked", instr)
	}
	if strings.Join(o.Argv, " ") != strings.Join([]string{fakeAgent, "report", "--prompt", instr}, " ") {
		t.Errorf("Argv %q", o.Argv)
	}
	if r.Stdin != "" {
		t.Errorf("custom agent got stdin %q", r.Stdin)
	}
	if !strings.Contains(readFile(t, o.LogPath), "brief too long to pass inline") {
		t.Error("log does not say the brief was too long to pass inline")
	}
}

// Scenario: Windows command line limit. A step with a 40,000-character brief
// starts, its command line is under 32,767 characters, and the agent
// receives the whole brief. It runs on every platform, and so on the Windows
// CI job, where the limit is real.
func TestWindowsCommandLineLimit(t *testing.T) {
	s := longStep(t, builtinAsFake("claude"), 40000)
	s.Env = []string{"FAKEAGENT_MODE=report"}
	o := run(t, s, fast)
	r := reportOf(t, o)
	brief := readFile(t, o.BriefPath)
	if n := utf8.RuneCountInString(brief); n < 40000 {
		t.Fatalf("brief is %d characters, want at least 40,000", n)
	}
	if n := windowsCmdLineLen(o.Argv); n >= 32767 {
		t.Errorf("command line is %d characters", n)
	}
	if r.Stdin != digest(brief) {
		t.Errorf("agent got %s, want the whole brief %s", r.Stdin, digest(brief))
	}
}

// Scenario: Log line. A Claude step starts with its brief inline: the log's
// start line shows <brief: N chars, …> where the brief would be, and the
// brief's text is only in the brief file. Run state's Argv shows the same.
func TestBriefLogLine(t *testing.T) {
	s := newStep(t, "build", "builder", 1, builtinAsFake("claude"))
	s.Env = []string{"FAKEAGENT_MODE=report"}
	o := run(t, s, fast)
	r := reportOf(t, o)
	brief := readFile(t, o.BriefPath)
	if len(r.Args) < 2 || r.Args[1] != digest(brief) {
		t.Fatalf("brief was not inline: %q", r.Args)
	}
	label := fmt.Sprintf("<brief: %d chars, %s>", utf8.RuneCountInString(brief), o.BriefPath)
	if len(o.Argv) < 3 || o.Argv[1] != "-p" || o.Argv[2] != label {
		t.Errorf("Argv %q, want %q after -p", o.Argv, label)
	}
	log := readFile(t, o.LogPath)
	var start string
	for _, l := range strings.Split(log, "\n") {
		if strings.Contains(l, "step build round 1 started") {
			start = l
		}
	}
	// The start line quotes arguments as quoteArgs does: strconv.Quote, which
	// escapes the backslashes of a Windows brief path.
	if want := quoteArgs([]string{label})[0]; !strings.Contains(start, want) {
		t.Errorf("start line %q lacks %q", start, want)
	}
	if strings.Contains(log, "You are running headless") {
		t.Errorf("brief text in the log:\n%s", log)
	}
}

func TestWindowsCmdLineLen(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		want int
	}{
		{[]string{"a", "b"}, 3},
		{[]string{"a", ""}, 4},
		{[]string{"a b"}, 5},   // "a b"
		{[]string{`a"b`}, 4},   // a\"b
		{[]string{`a\ b\`}, 8}, // "a\ b\\"
		{[]string{`x\"`}, 5},   // x\\\"
		{[]string{"é😀"}, 3},    // one + two UTF-16 units
		{[]string{"a\nb"}, 3},  // newlines are not quoted
	} {
		if got := windowsCmdLineLen(tc.argv); got != tc.want {
			t.Errorf("%q: %d, want %d", tc.argv, got, tc.want)
		}
	}
}
