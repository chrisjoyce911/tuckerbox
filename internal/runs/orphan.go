package runs

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// StoppedOrphan is the reason recorded when tuckerbox stop ends the agent
// of a run whose process had died.
const StoppedOrphan = "stopped by tuckerbox stop after the run process ended"

// ErrNoOrphan means the run has no agent terminal to stop: it is not
// running a step in a terminal.
var ErrNoOrphan = errors.New("the run has no agent terminal to stop")

// StopOrphan stops a run whose process died while its agent works on in a
// terminal (formal/terminals.tla, Stop with the lock free): it takes the
// change lock, ends the terminal by name with end, which must wait until
// it is gone, records the run stopped and lets the lock go. It returns
// ErrLocked when a process holds the lock (signal that process instead),
// and ErrNoOrphan when run.json names no terminal. Nothing is recorded
// when end fails.
func StopOrphan(ctx context.Context, st statedir.Dir, end func(ctx context.Context, name string) error) (*Run, error) {
	lock, err := TryChangeLock(st.RunLock())
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	rs, err := ReadRun(st.RunFile())
	if err != nil {
		return nil, err
	}
	if (rs.State != Running && rs.State != Queued) || rs.Terminal == nil {
		return rs, ErrNoOrphan
	}
	if err := end(ctx, rs.Terminal.Name); err != nil {
		return rs, err
	}
	now := time.Now()
	rs.State, rs.Reason, rs.Since = Stopped, StoppedOrphan, now
	rs.Pid, rs.AgentPid = 0, 0
	rs.Slot, rs.Terminal, rs.AgentState = nil, nil, ""
	rs.Tail = ""
	if err := WriteRun(st.RunFile(), rs); err != nil {
		return rs, err
	}
	ev := history.Event{Time: now, Kind: history.Stopped, Change: rs.Change, Step: rs.Step, Round: rs.Round,
		Reason: StoppedOrphan, Machine: history.Machine()}
	_ = history.Append(history.File(st), ev)
	_ = os.Remove(StopFile(st))
	return rs, nil
}

// AgentLive asks herdr-core (hc on the PATH) whether r's agent terminal
// lives and is r's current step and round. Without hc it is false.
func AgentLive(ctx context.Context, r *Run) bool {
	if r == nil || r.Terminal == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	t, err := agent.HC{}.Get(ctx, r.Terminal.Name)
	return err == nil && t.Live() && agent.MatchesStep(t, r.Change, r.Step, r.Round)
}

// Seen is the run as people should see it: Effective, except that a run
// whose process died while its agent works on in a terminal is shown
// running, not watched (View). watched is false then.
func Seen(ctx context.Context, r *Run, active bool) (v Run, watched bool) {
	return r.View(active, r.Orphaned(active) && AgentLive(ctx, r))
}
