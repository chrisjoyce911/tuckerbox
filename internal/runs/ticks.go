package runs

import (
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/scope"
)

// TickEvery is how often a run process reads tasks.md during a step
// (agent-activity: Ticks in history).
const TickEvery = 10 * time.Second

// tickWatch compares tasks.md with what it saw last and records each task
// whose box changed as a task_ticked or task_unticked event.
type tickWatch struct {
	path  string
	last  []scope.Task
	ok    bool
	event func(ev history.Event)
	step  string
	round int
}

// check reads tasks.md once. The first read is the baseline. A file that
// can't be read, or is missing (being replaced), is skipped; the next read
// compares with the last good one.
func (w *tickWatch) check() {
	tasks, err := readTasks(w.path)
	if err != nil || tasks == nil {
		return
	}
	if w.ok {
		numbers, ticked := scope.Changed(w.last, tasks)
		for i, n := range numbers {
			kind := history.TaskTicked
			if !ticked[i] {
				kind = history.TaskUnticked
			}
			w.event(history.Event{Kind: kind, Task: n, Step: w.step, Round: w.round})
		}
	}
	w.last, w.ok = tasks, true
}

// watchTicks reads the worktree's tasks.md every e.TickEvery while step
// runs, until the returned function is called; that function reads it a
// last time, so a tick made just before the agent ended is recorded too,
// and returns once the watcher has stopped. The caller holds the change
// lock, so the watcher is the history's one writer while the step's agent
// runs.
func (e *Engine) watchTicks(ws Workspace, rs *Run, step string, round int) func() {
	every := e.TickEvery
	if every <= 0 {
		every = TickEvery
	}
	change := rs.Change
	w := &tickWatch{path: TasksFile(ws.Path, change), step: step, round: round,
		event: func(ev history.Event) { e.event(ws, &Run{Change: change, Step: step, Round: round}, ev) }}
	if e.machine == "" {
		e.machine = history.Machine()
	}
	w.check()
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				w.check()
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
		w.check()
	}
}
