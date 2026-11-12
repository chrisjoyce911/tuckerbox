package edits

import (
	"context"
	"errors"
	"fmt"

	"github.com/chrisjoyce911/tuckerbox/internal/runs"
)

// ActiveRunError means the change has a queued or running run on this
// machine, so it can't be edited now.
type ActiveRunError struct {
	Change, Worktree string
	State            runs.State
	Step             string
	Round            int
	Pid              int
}

func (e *ActiveRunError) Error() string {
	var what string
	switch e.State {
	case runs.Queued:
		what = "queued"
		if e.Step != "" {
			what = "queued for step " + e.Step
		}
	case runs.Running:
		what = "running"
		if e.Step != "" {
			what = "running step " + e.Step
		}
	default:
		// Recorded as finished while its process still exits.
		what = fmt.Sprintf("finishing (%s)", e.State)
	}
	if e.Step != "" && e.Round > 0 && (e.State == runs.Queued || e.State == runs.Running) {
		what += fmt.Sprintf(" (round %d)", e.Round)
	}
	msg := fmt.Sprintf("change %s has an active run: %s in %s", e.Change, what, e.Worktree)
	if e.Pid > 0 {
		msg += fmt.Sprintf(" (pid %d)", e.Pid)
	}
	return msg + "; edit it once the run stops, blocks or finishes, or stop it with tuckerbox stop " + e.Change
}

func (e *ActiveRunError) Unwrap() error { return runs.ErrLocked }

// WithChangeLock runs fn while holding the change lock of p, the same lock
// a run holds while it is queued or running (<git-dir>/tuckerbox/run.lock;
// formal/runs.tla, OneRunPerChange). So no run starts, resumes or reruns
// while fn runs, and fn never runs while a run is active.
//
// command names the holder for others, such as "tuckerbox approve": a run,
// resume or edit that finds the lock taken says the change is being edited
// by it.
//
// It returns, without calling fn:
//   - an *ActiveRunError when a run holds the lock, naming its state and
//     step;
//   - a *runs.EditingError when another edit holds it.
//
// Both wrap runs.ErrLocked. Otherwise it returns fn's error. When p is the
// repository checkout (p.State == "") there is no lock to take: fn just
// runs.
//
// tuckerbox edit (while the editor is open), tasks, set, approve and unapprove
// call it. Its signature is kept stable for them.
func WithChangeLock(ctx context.Context, p Place, command string, fn func() error) error {
	if p.State == "" {
		return fn()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	lock, err := runs.TryChangeLock(p.State.RunLock())
	if errors.Is(err, runs.ErrLocked) {
		return lockedError(p)
	}
	if err != nil {
		return err
	}
	defer lock.Release()
	if err := runs.WriteEditMarker(p.State, command); err != nil {
		return fmt.Errorf("marking the change as being edited: %w", err)
	}
	defer runs.RemoveEditMarker(p.State)
	return fn()
}

// lockedError explains who holds p's lock.
func lockedError(p Place) error {
	err := runs.LockedError(p.Change, p.Dir, p.State)
	var editing *runs.EditingError
	if errors.As(err, &editing) {
		return err
	}
	e := &ActiveRunError{Change: p.Change, Worktree: p.Dir, State: runs.Running, Pid: runs.ReadPid(p.State)}
	if r, rerr := runs.ReadRun(p.State.RunFile()); rerr == nil {
		v := r.Effective(true)
		e.State, e.Step, e.Round = v.State, v.Step, v.Round
	}
	return e
}
