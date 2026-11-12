package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/board"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// boardCard reads `tuckerbox board --json` and returns id's card.
func (f *cliFixture) boardCard(id string) (board.Card, *board.Document) {
	f.t.Helper()
	res := f.tb("board", "--json")
	var doc board.Document
	if err := json.Unmarshal([]byte(res.stdout), &doc); err != nil {
		f.t.Fatalf("board: %v\n%s%s", err, res.stdout, res.stderr)
	}
	for _, c := range doc.Cards {
		if c.Change == id {
			return c, &doc
		}
	}
	f.t.Fatalf("no card %s", id)
	return board.Card{}, nil
}

// tickTask ticks task n in id's worktree tasks.md, as its agent would.
func (f *cliFixture) tickTask(id, n string) {
	f.t.Helper()
	p := runs.TasksFile(f.worktreeOf(id), id)
	data, err := os.ReadFile(p)
	if err != nil {
		f.t.Fatal(err)
	}
	testenv.WriteFile(f.t, p, strings.Replace(string(data), "- [ ] "+n+" ", "- [x] "+n+" ", 1))
}

// agent-activity: Activity in board data, "Card with activity", and
// Waiting for a person, "Permission prompt". add-login runs its build step
// in a terminal: its card's activity has the step, agent, task, last tick,
// times, agent state, backend terminal and the terminal's address on this
// machine. When herdr-core sees the agent blocked on a permission prompt,
// the card says waiting for you.
func TestCardWithActivity(t *testing.T) {
	f := newTermCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	testenv.WriteFile(t, runs.TasksFile(f.repo, "add-login"),
		"# Tasks\n\n## 2. Sign in\n\n- [ ] 2.1 Check the password\n- [ ] 2.2 Lock out\n- [ ] 2.3 Remember me\n")
	testenv.Git(t, f.repo, "add", "-A")
	testenv.Git(t, f.repo, "commit", "-q", "-m", "tasks")
	f.plan(map[string]testenv.Action{"build": {WaitFor: f.gate("build"), ExitAfter: true}})
	f.start("add-login")
	f.waitAgent("add-login", "build")
	f.tickTask("add-login", "2.1")
	f.tickTask("add-login", "2.2")
	// The run process reads tasks.md every 10 seconds.
	var c board.Card
	var doc *board.Document
	waitFor(t, "the tick of 2.2 on the card", func() bool {
		c, doc = f.boardCard("add-login")
		return c.Activity != nil && c.Activity.LastTick != nil && c.Activity.LastTick.Number == "2.2"
	})
	a := c.Activity
	r := f.runOf("add-login")
	if a.Step != "build" || a.Agent != "fake" || a.Round != r.Round || a.Backend != "terminal" || a.AgentState != "working" {
		t.Errorf("activity %+v", a)
	}
	if a.Task == nil || a.Task.Number != "2.3" || a.Task.Text != "Remember me" || a.TaskLabel != "on 2.3" {
		t.Errorf("task %+v %q", a.Task, a.TaskLabel)
	}
	if a.StepStarted == "" || a.RunStarted == "" || a.LastActivity == "" || a.LastTick.At == "" {
		t.Errorf("times %+v", a)
	}
	if a.Terminal == nil || a.Terminal.Address != r.Terminal.Address || a.Terminal.Address == "" || a.Terminal.Machine != doc.Machine || a.Terminal.Name != r.Terminal.Name {
		t.Errorf("terminal %+v, run %+v", a.Terminal, r.Terminal)
	}
	detail := f.tb("board", "--change", "add-login", "--json")
	var d board.Document
	if err := json.Unmarshal([]byte(detail.stdout), &d); err != nil || d.Detail == nil {
		t.Fatalf("detail: %v %s", err, detail.stderr)
	}
	if len(d.Detail.Ticks) != 2 || d.Detail.Ticks[0].Number != "2.2" || d.Detail.Ticks[1].Number != "2.1" {
		t.Errorf("ticks %+v, want 2.2 then 2.1", d.Detail.Ticks)
	}

	// Permission prompt: herdr-core reports the agent blocked on the person.
	if out, err := exec.Command(f.env.HC, "status", r.Terminal.Name, "blocked").CombinedOutput(); err != nil {
		t.Fatalf("hc status: %v %s", err, out)
	}
	if c, _ := f.boardCard("add-login"); c.Activity == nil || c.Activity.AgentState != "waiting for you" {
		t.Errorf("blocked agent: activity %+v", c.Activity)
	}
	// The run process sees it too and records it in run.json.
	waitFor(t, "run.json to say waiting for you", func() bool { return f.runOf("add-login").AgentState == "waiting for you" })
	if out, err := exec.Command(f.env.HC, "status", r.Terminal.Name, "working").CombinedOutput(); err != nil {
		t.Fatalf("hc status: %v %s", err, out)
	}
	waitFor(t, "the agent to work again", func() bool {
		c, _ := f.boardCard("add-login")
		return c.Activity != nil && c.Activity.AgentState == "working" && f.runOf("add-login").AgentState == "working"
	})
	f.open("build")
	f.waitState("add-login", "ready", "")
	if c, _ := f.boardCard("add-login"); c.Activity != nil {
		t.Errorf("ready card has activity %+v", c.Activity)
	}
}
