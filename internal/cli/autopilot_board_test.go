package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

type boardDoc struct {
	Autopilot struct {
		Running bool
		Pid     int
		Started string
	}
	Cards []struct {
		Change   string
		NeedsYou string `json:"needs_you"`
	}
}

func (f *cliFixture) board() boardDoc {
	f.t.Helper()
	res := f.tb("board", "--json")
	var d boardDoc
	if err := json.Unmarshal([]byte(res.stdout), &d); err != nil {
		f.t.Fatalf("board --json: %v\n%s\n%s", err, res.stdout, res.stderr)
	}
	return d
}

func (d boardDoc) needs(id string) string {
	for _, c := range d.Cards {
		if c.Change == id {
			return c.NeedsYou
		}
	}
	return "<no card>"
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// startAutopilot runs tuckerbox autopilot in the background until the
// test ends.
func (f *cliFixture) startAutopilot() (*exec.Cmd, *syncBuf) {
	f.t.Helper()
	cmd := exec.Command(tools.Tuckerbox, "autopilot")
	cmd.Dir = f.repo
	out := &syncBuf{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		f.t.Fatal(err)
	}
	stop := func() {
		if runtime.GOOS == "windows" {
			cmd.Process.Kill()
		} else {
			cmd.Process.Signal(os.Interrupt)
		}
		cmd.Wait()
	}
	f.t.Cleanup(stop)
	waitFor(f.t, "autopilot's marker", func() bool { return f.board().Autopilot.Running })
	return cmd, out
}

// Scenario: Autopilot running: the board has its pid and start time.
// Scenario: Blocked review: one line naming the change and reason, and it
// goes on starting other changes. Scenario: Leftover marker: killed, its
// marker left, the board says not running.
func TestAutopilotOnTheBoard(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"a", "b"}, RequireApproval: true})
	if d := f.board(); d.Autopilot.Running {
		t.Fatal("autopilot running before it started")
	}
	f.approveAll("a", "b")
	f.env.SetPlan(t, map[string]testenv.Action{
		"build":    {Tick: []string{"1.1"}},
		"a:review": {Status: "blocked", Summary: "review limit reached"},
	})
	cmd, out := f.startAutopilot()
	d := f.board()
	if d.Autopilot.Pid != cmd.Process.Pid || d.Autopilot.Started == "" {
		t.Errorf("autopilot %+v, pid %d", d.Autopilot, cmd.Process.Pid)
	}
	f.waitState("a", "blocked", "")
	f.waitState("b", "ready", "") // started by a later pass, after a's run ended
	time.Sleep(6 * time.Second)   // at least one more pass
	if n := strings.Count(out.String(), "a needs you: blocked: "); n != 1 {
		t.Errorf("a reported %d times:\n%s", n, out.String())
	}
	if !strings.Contains(out.String(), "start b --tasks next") {
		t.Errorf("b not started:\n%s", out.String())
	}
	if got := f.board().needs("a"); got != "blocked: step review is blocked: review limit reached" {
		t.Errorf("a needs_you %q", got)
	}

	if runtime.GOOS == "windows" {
		return
	}
	cmd.Process.Kill()
	cmd.Wait()
	if _, err := os.Stat(filepath.Join(f.repo, ".git", "tuckerbox", "autopilot.json")); err != nil {
		t.Fatal("no leftover marker after a kill")
	}
	if d := f.board(); d.Autopilot.Running {
		t.Errorf("leftover marker counted as running: %+v", d.Autopilot)
	}
}

// Scenario: Stale before the next group: autopilot says the change needs
// approval again before group 2. Scenario: Next group waits for approval:
// the card needs you. Scenario: Whole change left ready (board).
func TestAutopilotNeedsYouCards(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	f.tasks("add-login", scopeTasks)
	f.approveAll("add-login")
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Tick: []string{"1.1", "1.2", "1.3", "1.4"}}})
	f.pass()
	f.waitState("add-login", "ready", "")
	testenv.WriteFile(t, filepath.Join(f.worktreeOf("add-login"), "openspec", "changes", "add-login", "design.md"), "# Design\n\nEdited by build.\n")
	res := f.pass()
	if strings.Contains(res.stdout, "start ") || !strings.Contains(res.stdout, "add-login needs you: needs approval again before group 2") {
		t.Errorf("pass:\n%s", res.stdout)
	}
	if got := f.board().needs("add-login"); got != "approve again to run the next group" {
		t.Errorf("needs_you %q", got)
	}

	g := newCLI(t, testenv.RepoOptions{Changes: []string{"whole"}, RequireApproval: true, Extra: "\n[autopilot]\nscope = \"change\"\n"})
	g.tasks("whole", threeGroups)
	g.approveAll("whole")
	g.env.SetPlan(t, map[string]testenv.Action{"build": {Tick: []string{"1.1", "2.1", "3.1"}}})
	g.pass()
	g.waitState("whole", "ready", "")
	if got := g.board().needs("whole"); got != "tasks not ticked: 3.2; run it again by hand" {
		t.Errorf("whole needs_you %q", got)
	}
}
