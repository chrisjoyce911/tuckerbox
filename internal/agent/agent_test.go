package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// fakeAgent is the path of the test agent binary built by TestMain.
var fakeAgent string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tuckerbox-fakeagent")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	name := "fakeagent"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	fakeAgent = filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", fakeAgent, "./testdata/fakeagent")
	// The fake agent needs no cgo, whatever the race detector needs.
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building fake agent: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func fake(mode string, extra ...string) Template {
	return Template{Name: "fake", Command: append([]string{fakeAgent, mode}, extra...)}
}

// newStep returns a step in a fresh worktree holding change fix-login.
func newStep(t *testing.T, step, role string, round int, agent Template) Step {
	t.Helper()
	wt := t.TempDir()
	dir := filepath.Join(wt, "openspec", "changes", "fix-login")
	if err := os.MkdirAll(filepath.Join(dir, "specs", "auth"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"proposal.md", "tasks.md", filepath.Join("specs", "auth", "spec.md")} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("# "+f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Step{
		Change: "fix-login", Step: step, Role: role, Round: round, Agent: agent,
		Worktree: wt, State: statedir.Dir(filepath.Join(t.TempDir(), "tuckerbox")),
	}
}

var fast = Options{Poll: 50 * time.Millisecond, StopGrace: 2 * time.Second}

func run(t *testing.T, s Step, opts Options) Outcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	o, err := Run(ctx, s, opts)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func sameDir(a, b string) bool {
	ea, err1 := filepath.EvalSymlinks(a)
	eb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	return strings.EqualFold(filepath.Clean(ea), filepath.Clean(eb))
}

// Scenario: Custom agent. A repository defines agent omp with a command
// template and a step uses it: Tuckerbox starts that command in the change's
// worktree with the placeholders filled in.
func TestCustomAgent(t *testing.T) {
	omp := Template{Name: "omp", Command: []string{
		fakeAgent, "argv",
		"--brief", PhBrief, "--dir", PhWorkdir, "--result=" + PhResult,
		"--model", PhModel, "--effort", PhEffort, PhPrompt,
	}}
	s := newStep(t, "build", "builder", 1, omp)
	s.Model = "omp-large"
	o := run(t, s, fast)
	if o.Status() != Done {
		t.Fatalf("status %s: %s\n%s", o.Status(), o.FailureReason(), o.Tail)
	}
	var got struct {
		Args []string `json:"args"`
		WD   string   `json:"wd"`
	}
	if err := json.Unmarshal([]byte(o.Result.Summary), &got); err != nil {
		t.Fatal(err)
	}
	if !sameDir(got.WD, s.Worktree) {
		t.Errorf("agent ran in %s, want the worktree %s", got.WD, s.Worktree)
	}
	want := []string{
		"--brief", o.BriefPath, "--dir", s.Worktree, "--result=" + o.ResultPath,
		"--model", "omp-large", promptFor(o.BriefPath),
	}
	if strings.Join(got.Args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("args\n got %q\nwant %q", got.Args, want)
	}
	for _, a := range got.Args {
		if strings.Contains(a, "{") {
			t.Errorf("placeholder left in %q", a)
		}
	}
}

// Scenario: Build after rejection. The build step starts after the review
// step rejected round 1: its brief includes round 1's findings.
func TestBuildAfterRejection(t *testing.T) {
	review := newStep(t, "review", "reviewer", 1, fake("rejected"))
	o := run(t, review, fast)
	if o.Status() != Rejected || o.Result == nil || len(o.Result.Findings) != 1 {
		t.Fatalf("review outcome %+v", o)
	}

	build := review
	build.Step, build.Role, build.Round, build.Agent = "build", "builder", 2, fake("done")
	build.Earlier = []Result{*o.Result}
	bo := run(t, build, fast)
	if bo.Status() != Done {
		t.Fatalf("build: %s", bo.FailureReason())
	}
	brief := readFile(t, bo.BriefPath)
	for _, want := range []string{
		"## Findings from earlier rounds",
		"### review, round 1: rejected",
		"[high] `login.go`: password is logged",
		`"round": 2`,
		`"step": "build"`,
		"make your final message this JSON object",
		bo.ResultPath,
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief lacks %q:\n%s", want, brief)
		}
	}
}

// Scenario: Stale result from an earlier round. A result file names round 1
// while the step is on round 2: Tuckerbox ignores it and keeps waiting for
// the round 2 result.
func TestStaleResultIgnoredThenAccepted(t *testing.T) {
	s := newStep(t, "review", "reviewer", 2, fake("stale-then-done"))
	o := run(t, s, fast)
	if o.Result == nil || o.Result.Round != 2 || o.Status() != Done {
		t.Fatalf("want the round 2 result, got %+v (%s)", o.Result, o.FailureReason())
	}
	if len(o.Ignored) != 1 || !strings.Contains(o.Ignored[0], "stale result from round 1") {
		t.Errorf("ignored = %q", o.Ignored)
	}
	if log := readFile(t, o.LogPath); !strings.Contains(log, "ignored result file") {
		t.Errorf("stale file not logged:\n%s", log)
	}
}

func TestStaleOnlyIsNeverAccepted(t *testing.T) {
	s := newStep(t, "review", "reviewer", 2, fake("stale-only"))
	o := run(t, s, fast)
	if o.Result != nil || o.Status() != Failed {
		t.Fatalf("stale result accepted: %+v", o.Result)
	}
	if !strings.Contains(o.FailureReason(), "stale result from round 1") {
		t.Errorf("reason %q", o.FailureReason())
	}
}

// A result left at this round's path by an earlier attempt (before a stop,
// resume or rerun) is moved aside, not taken as this attempt's result.
func TestLeftoverResultNotAccepted(t *testing.T) {
	s := newStep(t, "build", "builder", 1, fake("noresult"))
	if err := s.State.Ensure(); err != nil {
		t.Fatal(err)
	}
	old := `{"change":"fix-login","step":"build","round":1,"status":"done","summary":"old"}`
	if err := os.WriteFile(s.State.Result("build", 1), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	o := run(t, s, fast)
	if o.Result != nil {
		t.Fatalf("leftover result accepted: %+v", o.Result)
	}
}

func TestMalformedResultIgnoredThenAccepted(t *testing.T) {
	s := newStep(t, "build", "builder", 1, fake("malformed-then-done"))
	o := run(t, s, fast)
	if o.Status() != Done {
		t.Fatalf("status %s: %s", o.Status(), o.FailureReason())
	}
	if len(o.Ignored) == 0 || !strings.Contains(o.Ignored[0], "malformed") {
		t.Errorf("ignored = %q", o.Ignored)
	}
}

func TestResultAtAnotherPathNotAccepted(t *testing.T) {
	s := newStep(t, "build", "builder", 1, fake("elsewhere"))
	if o := run(t, s, fast); o.Result != nil {
		t.Fatalf("accepted %+v", o.Result)
	}
}

// change-runs scenario: Agent exits without a result. The step is failed with
// the process's exit status and the tail of its output.
func TestExitWithoutResult(t *testing.T) {
	s := newStep(t, "build", "builder", 1, fake("noresult"))
	o := run(t, s, fast)
	if o.Status() != Failed || o.Result != nil {
		t.Fatalf("status %s", o.Status())
	}
	if o.ExitCode != 3 {
		t.Errorf("exit code %d, want 3", o.ExitCode)
	}
	if !strings.Contains(o.Tail, "fatal: boom") || !strings.Contains(o.Tail, "something went wrong") {
		t.Errorf("tail lacks the agent's output:\n%s", o.Tail)
	}
	if !strings.Contains(o.FailureReason(), "status 3") {
		t.Errorf("reason %q", o.FailureReason())
	}
}

func TestAgentNotFound(t *testing.T) {
	s := newStep(t, "build", "builder", 1, Template{Name: "nope", Command: []string{filepath.Join(t.TempDir(), "no-such-agent")}})
	if _, err := Run(context.Background(), s, fast); err == nil {
		t.Fatal("want an error for a missing agent program")
	}
}

// syncBuffer is a bytes.Buffer safe for FollowLog's writes and test reads.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// Scenario: Following a running step. The build agent's output appears as it
// is written.
func TestFollowRunningStep(t *testing.T) {
	s := newStep(t, "build", "builder", 1, fake("slow"))
	var finished sync.WaitGroup
	finished.Add(1)
	var mu sync.Mutex
	running := true
	go func() {
		defer finished.Done()
		o, err := Run(context.Background(), s, fast)
		if err != nil || o.Status() != Done {
			t.Errorf("run: %v %s", err, o.FailureReason())
		}
		mu.Lock()
		running = false
		mu.Unlock()
	}()
	isDone := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return !running
	}

	var out syncBuffer
	followed := make(chan error, 1)
	go func() {
		followed <- FollowLog(context.Background(), s.State.Log("build"), &out,
			FollowOptions{Poll: 20 * time.Millisecond, WaitForFile: true, Done: isDone})
	}()

	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(out.String(), "line 1\n") {
		if time.Now().After(deadline) {
			t.Fatalf("no output while running:\n%s", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if isDone() {
		t.Error("first line only appeared after the step finished")
	}
	if err := <-followed; err != nil {
		t.Fatal(err)
	}
	finished.Wait()
	if got := out.String(); !strings.Contains(got, "line 10\n") || !strings.Contains(got, "finished: exit 0, status done") {
		t.Errorf("follow missed the end of the log:\n%s", got)
	}
}

func TestStopEndsAgent(t *testing.T) {
	s := newStep(t, "build", "builder", 1, fake("hang"))
	ctx, cancel := context.WithCancel(context.Background())
	var pid int
	opts := fast
	opts.OnStart = func(p int) { pid = p }
	time.AfterFunc(300*time.Millisecond, cancel)
	start := time.Now()
	o, err := Run(ctx, s, opts)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 15*time.Second {
		t.Errorf("stop took %s", time.Since(start))
	}
	if !o.Stopped || o.Status() != Failed || pid == 0 {
		t.Errorf("stopped=%v status=%s pid=%d", o.Stopped, o.Status(), pid)
	}
}

func TestAgentLingeringAfterResultIsStopped(t *testing.T) {
	s := newStep(t, "build", "builder", 1, fake("linger"))
	opts := fast
	opts.ResultGrace = 300 * time.Millisecond
	start := time.Now()
	o := run(t, s, opts)
	if o.Status() != Done {
		t.Fatalf("status %s", o.Status())
	}
	if time.Since(start) > 15*time.Second {
		t.Errorf("lingering agent ran for %s", time.Since(start))
	}
}

func TestLogAppendsRounds(t *testing.T) {
	s := newStep(t, "build", "builder", 1, fake("done"))
	run(t, s, fast)
	s.Round = 2
	run(t, s, fast)
	log := readFile(t, s.State.Log("build"))
	if !strings.Contains(log, "step build round 1 started") || !strings.Contains(log, "step build round 2 started") {
		t.Errorf("log does not hold both rounds:\n%s", log)
	}
	var buf bytes.Buffer
	if err := ReadLog(s.State.Log("build"), &buf); err != nil || buf.String() != log {
		t.Errorf("ReadLog: %v", err)
	}
}

func TestMissingLog(t *testing.T) {
	p := filepath.Join(t.TempDir(), "none.log")
	if err := ReadLog(p, &bytes.Buffer{}); !errors.Is(err, ErrNoLog) {
		t.Errorf("ReadLog: %v", err)
	}
	if _, err := TailLog(p, 10); !errors.Is(err, ErrNoLog) {
		t.Errorf("TailLog: %v", err)
	}
	if err := FollowLog(context.Background(), p, &bytes.Buffer{}, FollowOptions{}); !errors.Is(err, ErrNoLog) {
		t.Errorf("FollowLog: %v", err)
	}
}

func TestBadStepID(t *testing.T) {
	s := newStep(t, "../escape", "builder", 1, fake("done"))
	if _, err := Run(context.Background(), s, fast); err == nil {
		t.Fatal("want an error for a step id with a path in it")
	}
}

// Scenario: Result in the final message. An agent that cannot write outside
// its worktree ends with the result JSON as its final message: Tuckerbox
// accepts it and writes the result file itself.
func TestResultFromFinalMessage(t *testing.T) {
	cases := []struct {
		name  string
		agent func(t *testing.T) Template
		want  Status
	}{
		{"bare object on stdout", func(*testing.T) Template { return fake("message") }, Done},
		{"fenced block on stdout", func(*testing.T) Template { return fake("message-fenced") }, Rejected},
		{"claude stream-json result event", func(*testing.T) Template { return fake("stream-json") }, Done},
		{"codex json agent message", func(*testing.T) Template { return fake("codex-jsonl") }, Done},
		{"lastmessage file", func(*testing.T) Template { return fake("lastmessage", PhLastMessage) }, Done},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newStep(t, "review", "reviewer", 2, c.agent(t))
			o := run(t, s, fast)
			if o.Status() != c.want {
				t.Fatalf("status %s, want %s: %s\n%s", o.Status(), c.want, o.FailureReason(), readFile(t, o.LogPath))
			}
			if o.Source != "final message" || o.Result.Round != 2 || o.Result.Step != "review" {
				t.Errorf("source %q, result %+v", o.Source, o.Result)
			}
			// The engine wrote the result file from the message.
			got, err := ReadResult(o.ResultPath, Expect{"fix-login", "review", 2})
			if err != nil {
				t.Fatalf("result file not written: %v", err)
			}
			if !sameResult(got, *o.Result) {
				t.Errorf("result file %+v, want %+v", got, *o.Result)
			}
			if !strings.Contains(readFile(t, o.LogPath), "accepted result from the final message") {
				t.Error("log does not say where the result came from")
			}
		})
	}
	t.Run("lastmessage is a Tuckerbox file outside the worktree", func(t *testing.T) {
		s := newStep(t, "review", "reviewer", 1, fake("lastmessage", PhLastMessage))
		o := run(t, s, fast)
		if o.MessagePath != s.State.LastMessage("review", 1) {
			t.Errorf("message path %q", o.MessagePath)
		}
		if o.Result == nil || o.Result.Summary != "from the last-message file" {
			t.Errorf("result %+v", o.Result)
		}
	})
}

// A {lastmessage} file left by an earlier attempt of the same round is
// removed before the agent starts, never taken as this attempt's message.
func TestLeftoverLastMessageNotAccepted(t *testing.T) {
	tpl := Template{Name: "fake", Command: []string{fakeAgent, "noresult", "-o", PhLastMessage}}
	s := newStep(t, "review", "reviewer", 1, tpl)
	if err := s.State.Ensure(); err != nil {
		t.Fatal(err)
	}
	old := `{"change":"fix-login","step":"review","round":1,"status":"done","summary":"old"}`
	if err := os.WriteFile(s.State.LastMessage("review", 1), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if o := run(t, s, fast); o.Result != nil {
		t.Fatalf("leftover final message accepted: %+v", o.Result)
	}
}

// Scenario: Malformed final message. The agent's final message holds no
// well-formed result and there is no result file: the step fails, and why
// is recorded.
func TestMalformedFinalMessage(t *testing.T) {
	s := newStep(t, "review", "reviewer", 1, fake("message-malformed"))
	o := run(t, s, fast)
	if o.Result != nil || o.Status() != Failed {
		t.Fatalf("accepted %+v", o.Result)
	}
	if !strings.Contains(o.FailureReason(), "final message: malformed result") {
		t.Errorf("reason %q", o.FailureReason())
	}
	if !strings.Contains(readFile(t, o.LogPath), "ignored final message") {
		t.Error("malformed final message not logged")
	}
	if _, err := os.Stat(o.ResultPath); err == nil {
		t.Error("a result file was written for a malformed message")
	}
}

// The round and step check applies to a result in the final message too
// (StaleNeverAccepted).
func TestStaleFinalMessageNotAccepted(t *testing.T) {
	for mode, want := range map[string]string{
		"message-stale":      "stale result from round 1",
		"message-wrong-step": `result is for step "other"`,
	} {
		t.Run(mode, func(t *testing.T) {
			s := newStep(t, "review", "reviewer", 2, fake(mode))
			o := run(t, s, fast)
			if o.Result != nil {
				t.Fatalf("accepted %+v", o.Result)
			}
			if !strings.Contains(o.FailureReason(), want) {
				t.Errorf("reason %q, want %q", o.FailureReason(), want)
			}
		})
	}
}

// Scenario: Result file and final message differ. The agent writes a
// result file and ends with a different result: the file wins, and the
// difference is logged.
func TestResultFileAndFinalMessageDiffer(t *testing.T) {
	s := newStep(t, "review", "reviewer", 1, fake("both-differ"))
	o := run(t, s, fast)
	if o.Status() != Done || o.Source != "result file" {
		t.Fatalf("status %s from %q, want done from the result file", o.Status(), o.Source)
	}
	if log := readFile(t, o.LogPath); !strings.Contains(log, "result file and final message differ; using the result file") {
		t.Errorf("difference not logged:\n%s", log)
	}

	s = newStep(t, "review", "reviewer", 1, fake("both-same"))
	o = run(t, s, fast)
	if o.Status() != Done {
		t.Fatalf("status %s", o.Status())
	}
	if strings.Contains(readFile(t, o.LogPath), "differ") {
		t.Error("identical results logged as different")
	}
}
