package runs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// fakeClock is a clock a test moves by hand.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// tickingBackend ticks tasks in the worktree's tasks.md during the build
// step as an agent would, moving the clock between ticks, and waits each
// time until the run process has recorded the tick; then it runs the step
// headless.
type tickingBackend struct {
	t     *testing.T
	clock *fakeClock
	gap   time.Duration
	ticks []string
}

func (b *tickingBackend) Name() string { return "ticking" }

func (b *tickingBackend) Run(ctx context.Context, s agent.Step, opts agent.Options) (agent.Outcome, error) {
	if s.Step == "build" {
		p := TasksFile(s.Worktree, s.Change)
		for i, n := range b.ticks {
			if i > 0 {
				b.clock.Advance(b.gap)
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return agent.Outcome{}, err
			}
			if err := os.WriteFile(p, []byte(strings.Replace(string(data), "- [ ] "+n+" ", "- [x] "+n+" ", 1)), 0o644); err != nil {
				return agent.Outcome{}, err
			}
			deadline := time.Now().Add(10 * time.Second)
			for !hasTick(b.t, s.State, n) {
				if time.Now().After(deadline) {
					b.t.Errorf("no task_ticked for %s", n)
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
	}
	return agent.Headless{}.Run(ctx, s, opts)
}

func hasTick(t *testing.T, st interface{ RunFile() string }, n string) bool {
	evs, err := history.Read(filepath.Join(filepath.Dir(st.RunFile()), history.FileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.Kind == history.TaskTicked && e.Task == n {
			return true
		}
	}
	return false
}

// agent-activity: Ticks in history, "Two ticks". The build agent ticks 2.1,
// then 2.2 four minutes later: history has task_ticked for 2.1 and for
// 2.2, four minutes apart, both with step build.
func TestTwoTicks(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	testenv.WriteFile(t, TasksFile(f.repo.Root, "fix-login"),
		"# Tasks\n\n## 2. Engine\n\n- [ ] 2.1 Add the field\n- [ ] 2.2 Start again\n- [ ] 2.3 Check ticks\n")
	testenv.Git(t, f.repo.Root, "add", "-A")
	testenv.Git(t, f.repo.Root, "commit", "-q", "-m", "tasks")

	clock := &fakeClock{t: time.Date(2026, 11, 13, 9, 0, 0, 0, time.FixedZone("AEST", 10*3600))}
	e := f.engine()
	e.Now = clock.Now
	e.TickEvery = 10 * time.Millisecond
	e.Backend = &tickingBackend{t: t, clock: clock, gap: 4 * time.Minute, ticks: []string{"2.1", "2.2"}}
	r, err := e.Run(context.Background(), Request{Change: "fix-login"})
	if err != nil || r.State != Ready {
		t.Fatalf("run: %v %+v", err, r)
	}
	var ticks []history.Event
	for _, ev := range f.history("fix-login") {
		if ev.Kind == history.TaskTicked || ev.Kind == history.TaskUnticked {
			ticks = append(ticks, ev)
		}
	}
	if len(ticks) != 2 || ticks[0].Task != "2.1" || ticks[1].Task != "2.2" {
		t.Fatalf("tick events %+v, want task_ticked 2.1 and 2.2", ticks)
	}
	for _, ev := range ticks {
		if ev.Kind != history.TaskTicked || ev.Step != "build" || ev.Round != 1 || ev.Agent == "" || ev.Change != "fix-login" {
			t.Errorf("event %+v, want task_ticked in build round 1", ev)
		}
	}
	if gap := ticks[1].Time.Sub(ticks[0].Time); gap != 4*time.Minute {
		t.Errorf("ticks %s apart, want 4m", gap)
	}
}

// A box unticked during a step is a task_unticked event; a tick made just
// before the agent ends is still recorded when the step ends.
func TestTickWatchUntickAndLastLook(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tasks.md")
	testenv.WriteFile(t, p, "- [x] 1.1 A\n- [ ] 1.2 B\n")
	var got []string
	w := &tickWatch{path: p, step: "build", round: 3, event: func(ev history.Event) {
		got = append(got, string(ev.Kind)+" "+ev.Task+" "+ev.Step)
	}}
	w.check()
	testenv.WriteFile(t, p, "- [ ] 1.1 A\n- [x] 1.2 B\n")
	w.check()
	os.Remove(p)
	w.check() // unreadable: skipped
	testenv.WriteFile(t, p, "- [ ] 1.1 A\n- [x] 1.2 B\n")
	w.check()
	if strings.Join(got, ", ") != "task_unticked 1.1 build, task_ticked 1.2 build" {
		t.Fatalf("events %v", got)
	}
}
