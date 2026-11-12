package runs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// An edit (tuckerbox edit, tasks, approve, unapprove) holds the same change
// lock as a run while it writes, so the two never overlap
// (formal/runs.tla, OneRunPerChange). While it holds the lock it keeps an
// edit marker next to it, so a run or resume that finds the lock taken can
// say the change is being edited rather than running.
//
// The marker means something only while the lock is held. A run that takes
// the lock removes any marker an edit that died left behind.

// EditMarker is the content of the edit marker file.
type EditMarker struct {
	// Command is what holds the lock, such as "tuckerbox edit design".
	Command string    `json:"command"`
	Pid     int       `json:"pid"`
	Since   time.Time `json:"since"`
}

// EditFile is the edit marker in a state directory.
func EditFile(d statedir.Dir) string { return filepath.Join(string(d), "edit.json") }

// WriteEditMarker records that command holds the change lock of d. The
// caller holds the lock.
func WriteEditMarker(d statedir.Dir, command string) error {
	data, err := json.Marshal(EditMarker{Command: command, Pid: os.Getpid(), Since: time.Now()})
	if err != nil {
		return err
	}
	return writeAtomic(EditFile(d), append(data, '\n'))
}

// RemoveEditMarker removes the edit marker of d. The caller holds the lock.
func RemoveEditMarker(d statedir.Dir) { _ = os.Remove(EditFile(d)) }

// ReadEditMarker returns the edit marker of d, if there is one. Call it
// only after finding the change lock held.
func ReadEditMarker(d statedir.Dir) (*EditMarker, bool) {
	data, err := os.ReadFile(EditFile(d))
	if err != nil {
		return nil, false
	}
	var m EditMarker
	if err := json.Unmarshal(data, &m); err != nil {
		// Half written, or broken: still an edit holds the lock.
		return &EditMarker{Command: "an edit"}, true
	}
	return &m, true
}

// EditingError means an edit holds the change lock, so a run can't start.
type EditingError struct {
	Change, Worktree string
	Marker           EditMarker
}

func (e *EditingError) Error() string {
	who := e.Marker.Command
	if who == "" {
		who = "an edit"
	}
	if e.Marker.Pid > 0 {
		who += fmt.Sprintf(", pid %d", e.Marker.Pid)
	}
	return fmt.Sprintf("change %s is being edited (%s) in %s; try again when the edit is finished", e.Change, who, e.Worktree)
}

func (e *EditingError) Unwrap() error { return ErrLocked }

// LockedError returns the error for a change whose lock at d is held by
// another process: an *EditingError when an edit holds it, else an
// *ActiveError.
func LockedError(change, worktree string, d statedir.Dir) error {
	if m, ok := ReadEditMarker(d); ok {
		return &EditingError{Change: change, Worktree: worktree, Marker: *m}
	}
	pid, _ := readPid(d.PidFile())
	return &ActiveError{Change: change, Worktree: worktree, Pid: pid}
}
