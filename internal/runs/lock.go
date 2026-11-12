package runs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

// ErrLocked means another process holds the change lock: the change already
// has an active run on this machine.
var ErrLocked = errors.New("change already has an active run")

// lockRetries and lockRetryWait let TryChangeLock ride out a brief probe of
// the lock by status or stop (see Active). Every attempt is one atomic
// try-lock; the lock is never checked first and taken second.
const (
	lockRetries   = 4
	lockRetryWait = 50 * time.Millisecond
)

// ChangeLock is a held change lock: an exclusive OS file lock on
// <git-dir>/tuckerbox/run.lock (formal/runs.tla, action Pick).
type ChangeLock struct{ f *flock.Flock }

// TryChangeLock takes the change lock at path, or returns ErrLocked when
// another process holds it. It never waits for a live holder.
func TryChangeLock(path string) (*ChangeLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	for i := 0; ; i++ {
		f := flock.New(path)
		ok, err := f.TryLock()
		if err != nil {
			return nil, fmt.Errorf("locking %s: %w", path, err)
		}
		if ok {
			return &ChangeLock{f: f}, nil
		}
		if i == lockRetries {
			return nil, ErrLocked
		}
		time.Sleep(lockRetryWait)
	}
}

// Release frees the lock. The OS frees it too if the process dies.
func (l *ChangeLock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Unlock()
	l.f = nil
	return err
}

// Active reports whether some process holds the change lock at path. It
// takes the lock for an instant when it is free, so a run starting at that
// very moment retries (see TryChangeLock).
func Active(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	f := flock.New(path)
	ok, err := f.TryLock()
	if err != nil {
		return false
	}
	if ok {
		_ = f.Unlock()
		return false
	}
	return true
}

// Slot is a held parallel slot: an exclusive lock on
// <common-dir>/tuckerbox/slots/<n>.lock with n < max_parallel
// (formal/runs.tla, action TakeSlot).
type Slot struct {
	N int
	f *flock.Flock
}

// SlotDir is the slot lock directory under the repository's common
// Tuckerbox directory.
func SlotDir(common string) string { return filepath.Join(common, "slots") }

// TrySlot takes any free slot n < max, or returns nil when all are taken.
func TrySlot(common string, max int) (*Slot, error) {
	dir := SlotDir(common)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	for n := 0; n < max; n++ {
		f := flock.New(filepath.Join(dir, strconv.Itoa(n)+".lock"))
		ok, err := f.TryLock()
		if err != nil {
			return nil, fmt.Errorf("locking slot %d: %w", n, err)
		}
		if ok {
			return &Slot{N: n, f: f}, nil
		}
	}
	return nil, nil
}

// Release frees the slot.
func (s *Slot) Release() error {
	if s == nil || s.f == nil {
		return nil
	}
	err := s.f.Unlock()
	s.f = nil
	return err
}

// Queue orders the runs of one repository that wait for a slot, so they
// start in the order they were requested. It only orders: the slot locks
// alone bound how many runs run (WithinLimit).
//
// A waiting run has a ticket file <common>/tuckerbox/queue/<at>-<change>
// holding the path of its change lock. A ticket is live while that change
// lock is held; the run holds it for as long as it exists, so a run that
// dies leaves a dead ticket, which the next waiter removes. Only the
// oldest live ticket may take a slot.
type Queue struct {
	dir    string
	ticket string
}

// QueueDir is the queue directory under the repository's common
// Tuckerbox directory.
func QueueDir(common string) string { return filepath.Join(common, "queue") }

// JoinQueue adds a ticket for change, requested at time at (nanoseconds),
// whose change lock (held by the caller) is at lockPath. Old tickets of the
// same change are dead, since the caller holds its lock, and are removed.
func JoinQueue(common, change, lockPath string, at int64) (*Queue, error) {
	dir := QueueDir(common)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if _, c, ok := parseTicket(e.Name()); ok && c == change {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	name := fmt.Sprintf("%020d-%s", at, change)
	tmp := filepath.Join(dir, "."+name+".tmp")
	if err := os.WriteFile(tmp, []byte(lockPath), 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return &Queue{dir: dir, ticket: name}, nil
}

func parseTicket(name string) (at int64, change string, ok bool) {
	if strings.HasPrefix(name, ".") {
		return 0, "", false
	}
	ts, change, found := strings.Cut(name, "-")
	if !found || change == "" {
		return 0, "", false
	}
	at, err := strconv.ParseInt(ts, 10, 64)
	return at, change, err == nil
}

// Head reports whether this ticket is the oldest live one. It removes dead
// tickets ahead of it.
func (q *Queue) Head() bool {
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		return true
	}
	var names []string
	for _, e := range entries {
		if _, _, ok := parseTicket(e.Name()); ok {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		if n >= q.ticket {
			break
		}
		p := filepath.Join(q.dir, n)
		data, err := os.ReadFile(p)
		if err != nil {
			continue // removed meanwhile
		}
		if Active(strings.TrimSpace(string(data))) {
			return false
		}
		_ = os.Remove(p)
	}
	return true
}

// Leave removes the ticket.
func (q *Queue) Leave() {
	if q != nil {
		_ = os.Remove(filepath.Join(q.dir, q.ticket))
	}
}

// WaitSlot waits, in queue order, for a free slot. It calls onWait once if
// it has to wait. It returns ctx's error if ctx ends first.
func WaitSlot(ctx context.Context, common string, max int, q *Queue, poll time.Duration, onWait func()) (*Slot, error) {
	waited := false
	for {
		if q == nil || q.Head() {
			s, err := TrySlot(common, max)
			if err != nil || s != nil {
				return s, err
			}
		}
		if !waited && onWait != nil {
			onWait()
		}
		waited = true
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(poll):
		}
	}
}
