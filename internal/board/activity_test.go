package board

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/scope"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

func activityConfig() *config.Config {
	return &config.Config{
		Steps:  []config.Step{{ID: "build", Agent: "claude", Effort: "high"}, {ID: "review", Agent: "codex"}},
		Agents: map[string]config.Agent{"claude": {Name: "claude", Model: "opus"}, "codex": {Name: "codex", Model: "gpt-5.5", Effort: "medium"}},
	}
}

func noTerminal(t *testing.T) {
	old := lookTerminal
	lookTerminal = func(context.Context, string) (*agent.HCTerminal, error) { return nil, errors.New("no herdr-core") }
	t.Cleanup(func() { lookTerminal = old })
}

// agent-activity: Times and last activity, "Quiet agent". A headless build
// step started 25 minutes ago whose agent last wrote output 12 minutes ago:
// the activity has the step's start and the log's last write as the last
// activity, which the card shows as running 25 minutes, last activity 12
// minutes ago.
func TestQuietAgent(t *testing.T) {
	noTerminal(t)
	dir := t.TempDir()
	st := statedir.Dir(filepath.Join(dir, "state"))
	folder := filepath.Join(dir, "change")
	testenv.WriteFile(t, filepath.Join(folder, "tasks.md"), "## 2. Sign in\n\n- [x] 2.1 Check the password\n- [x] 2.2 Lock out\n- [ ] 2.3 Remember me\n")
	now := time.Now()
	stepStart, quiet := now.Add(-25*time.Minute), now.Add(-12*time.Minute)
	testenv.WriteFile(t, st.Log("build"), "working\n")
	if err := os.Chtimes(st.Log("build"), quiet, quiet); err != nil {
		t.Fatal(err)
	}
	events := []history.Event{
		{Kind: history.Started, Time: now.Add(-40 * time.Minute), Step: "build", Round: 1},
		{Kind: history.StepStarted, Time: now.Add(-38 * time.Minute), Step: "build", Round: 1},
		{Kind: history.TaskTicked, Time: now.Add(-30 * time.Minute), Step: "build", Round: 1, Task: "2.1"},
		{Kind: history.Rejected, Time: now.Add(-26 * time.Minute), Step: "review", Round: 1},
		{Kind: history.StepStarted, Time: stepStart, Step: "build", Round: 2},
		{Kind: history.TaskTicked, Time: now.Add(-20 * time.Minute), Step: "build", Round: 2, Task: "2.2"},
	}
	r := &runs.Run{Change: "add-login", State: runs.Running, Step: "build", Round: 2, AgentBackend: "headless",
		Scope: &scope.Scope{Spec: "2", Tasks: []string{"2.1", "2.2", "2.3"}}, Since: stepStart}
	a := readActivity(context.Background(), activityConfig(), r, folder, st, events, "mini")
	if a.StepStarted != timestamp(stepStart) || a.LastActivity != timestamp(quiet) || a.RunStarted != timestamp(events[0].Time) {
		t.Errorf("times: step %s run %s last %s", a.StepStarted, a.RunStarted, a.LastActivity)
	}
	if a.Step != "build" || a.Round != 2 || a.Agent != "claude" || a.Model != "opus" || a.Effort != "high" {
		t.Errorf("activity %+v", a)
	}
	if a.Task == nil || a.Task.Number != "2.3" || a.Task.Text != "Remember me" || a.TaskLabel != "on 2.3" {
		t.Errorf("task %+v %q", a.Task, a.TaskLabel)
	}
	if a.LastTick == nil || a.LastTick.Number != "2.2" || a.LastTick.At != timestamp(events[5].Time) {
		t.Errorf("last tick %+v", a.LastTick)
	}
	if a.AgentState != "working" || a.Backend != "headless" || a.Terminal != nil {
		t.Errorf("state %q backend %q terminal %+v", a.AgentState, a.Backend, a.Terminal)
	}
	// Times carry their offset.
	if _, err := time.Parse(time.RFC3339, a.LastActivity); err != nil {
		t.Error(err)
	}
}

// agent-activity: Times and last activity. The run process crashed during
// build round 2 and resume adopted the same agent, recording step_started
// again: the step's running time still counts from the attempt's start. A
// step that ended and started again in the same round counts from the new
// start.
func TestActivityStepStartAcrossAdoption(t *testing.T) {
	noTerminal(t)
	dir := t.TempDir()
	st := statedir.Dir(filepath.Join(dir, "state"))
	folder := filepath.Join(dir, "change")
	testenv.WriteFile(t, filepath.Join(folder, "tasks.md"), "## 2. Sign in\n\n- [ ] 2.1 Check the password\n")
	now := time.Now()
	start, adopted := now.Add(-40*time.Minute), now.Add(-5*time.Minute)
	events := []history.Event{
		{Kind: history.Started, Time: start.Add(-time.Minute), Step: "build", Round: 2},
		{Kind: history.StepStarted, Time: start, Step: "build", Round: 2},
		{Kind: history.Resumed, Time: adopted, Step: "build", Round: 2},
		{Kind: history.StepStarted, Time: adopted, Step: "build", Round: 2},
	}
	r := &runs.Run{Change: "add-login", State: runs.Running, Step: "build", Round: 2, AgentBackend: "headless", Since: adopted}
	if a := readActivity(context.Background(), activityConfig(), r, folder, st, events, ""); a.StepStarted != timestamp(start) {
		t.Errorf("adopted: step started %s, want %s", a.StepStarted, timestamp(start))
	}
	again := now.Add(-2 * time.Minute)
	events = append(events,
		history.Event{Kind: history.StepFinished, Time: now.Add(-3 * time.Minute), Step: "build", Round: 2},
		history.Event{Kind: history.StepStarted, Time: again, Step: "build", Round: 2})
	if a := readActivity(context.Background(), activityConfig(), r, folder, st, events, ""); a.StepStarted != timestamp(again) {
		t.Errorf("new attempt: step started %s, want %s", a.StepStarted, timestamp(again))
	}
}

// agent-activity: The task a run is on, "Everything in scope ticked", as the
// card shows it.
func TestActivityAllTicked(t *testing.T) {
	noTerminal(t)
	dir := t.TempDir()
	folder := filepath.Join(dir, "change")
	testenv.WriteFile(t, filepath.Join(folder, "tasks.md"), "- [x] 2.1 A\n- [x] 2.2 B\n- [ ] 3.1 Later\n")
	r := &runs.Run{Change: "add-login", State: runs.Running, Step: "review", Round: 1, Since: time.Now(),
		Scope: &scope.Scope{Spec: "2", Tasks: []string{"2.1", "2.2"}}}
	a := readActivity(context.Background(), activityConfig(), r, folder, "", nil, "mini")
	if a.Task != nil || a.TaskLabel != scope.AllTicked || a.Model != "gpt-5.5" || a.Effort != "medium" {
		t.Errorf("activity %+v", a)
	}
	if a.LastTick != nil {
		t.Errorf("last tick %+v without ticks", a.LastTick)
	}
}

// A terminal agent's activity comes from herdr-core's record: its last
// output, and waiting for you when herdr-core sees it blocked on the
// person, as well as when its turn ended without a result.
func TestActivityFromTheTerminal(t *testing.T) {
	out := time.Now().Add(-3 * time.Minute).Truncate(time.Millisecond)
	ms := out.UnixMilli()
	status := &agent.HCStatus{State: "working"}
	old := lookTerminal
	lookTerminal = func(_ context.Context, name string) (*agent.HCTerminal, error) {
		return &agent.HCTerminal{Name: name, Address: "t7", State: "live", Status: status, OutputMs: &ms}, nil
	}
	t.Cleanup(func() { lookTerminal = old })
	r := &runs.Run{Change: "add-login", State: runs.Running, Step: "build", Round: 1, Since: time.Now(), AgentBackend: "terminal",
		Terminal: &runs.TerminalRef{Name: "tb-abc"}, AgentState: "working"}
	a := readActivity(context.Background(), activityConfig(), r, t.TempDir(), "", nil, "mini")
	if a.Backend != "terminal" || a.Terminal == nil || a.Terminal.Address != "t7" || a.Terminal.Machine != "mini" || a.Terminal.Name != "tb-abc" {
		t.Fatalf("terminal %+v backend %q", a.Terminal, a.Backend)
	}
	if a.LastActivity != timestamp(out) || a.AgentState != "working" {
		t.Errorf("last %s state %q", a.LastActivity, a.AgentState)
	}
	status.State = "blocked"
	if a := readActivity(context.Background(), activityConfig(), r, t.TempDir(), "", nil, "mini"); a.AgentState != agent.AgentWaiting {
		t.Errorf("blocked terminal: state %q", a.AgentState)
	}
	status.State = "done"
	r.AgentState = agent.AgentWaiting
	if a := readActivity(context.Background(), activityConfig(), r, t.TempDir(), "", nil, "mini"); a.AgentState != agent.AgentWaiting {
		t.Errorf("turn ended without a result: state %q", a.AgentState)
	}
	// A stopped run has activity but no agent state or terminal.
	r.State, r.Terminal = runs.Stopped, nil
	if a := readActivity(context.Background(), activityConfig(), r, t.TempDir(), "", nil, "mini"); a.AgentState != "" || a.Terminal != nil {
		t.Errorf("stopped: %+v", a)
	}
}
