package runs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gofrs/flock"

	"github.com/chrisjoyce911/tuckerbox/internal/priority"
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
//
// Each slot also has an agent lock, <n>.agent.lock, which tuckerbox
// agent-hold takes in an agent's terminal for as long as the agent lives.
// An agent in a terminal outlives the run process that started it; when
// that process dies the OS frees the slot's run lock but not its agent
// lock, so the slot stays taken (formal/liveslots.tla, OrphanHoldsSlot).
// Admission takes a slot only when both are free (WithinLimitLive).
type Slot struct {
	N int
	f *flock.Flock
}

// SlotDir is the slot lock directory under the repository's common
// Tuckerbox directory.
func SlotDir(common string) string { return filepath.Join(common, "slots") }

// SlotLock is slot n's run lock file.
func SlotLock(common string, n int) string {
	return filepath.Join(SlotDir(common), strconv.Itoa(n)+".lock")
}

// AgentLock is slot n's agent lock file.
func AgentLock(common string, n int) string {
	return filepath.Join(SlotDir(common), strconv.Itoa(n)+".agent.lock")
}

// TrySlot takes any free slot n < max, or returns nil when all are taken.
// A slot is free when its run lock and its agent lock are both free: the
// run lock is taken, then the agent lock tried; if an agent holds it the
// run lock is let go and the next slot tried (liveslots.tla, Admit). The
// agent lock is only tested, never kept: the run lock keeps every other
// admission off the slot until this run's own agent takes it.
func TrySlot(common string, max int) (*Slot, error) {
	dir := SlotDir(common)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	for n := 0; n < max; n++ {
		f := flock.New(SlotLock(common, n))
		ok, err := f.TryLock()
		if err != nil {
			return nil, fmt.Errorf("locking slot %d: %w", n, err)
		}
		if !ok {
			continue
		}
		free, err := agentLockFree(common, n)
		if err != nil {
			_ = f.Unlock()
			return nil, err
		}
		if !free {
			_ = f.Unlock()
			continue
		}
		return &Slot{N: n, f: f}, nil
	}
	return nil, nil
}

// agentLockFree tests slot n's agent lock by taking and freeing it.
func agentLockFree(common string, n int) (bool, error) {
	a := flock.New(AgentLock(common, n))
	ok, err := a.TryLock()
	if err != nil {
		return false, fmt.Errorf("locking slot %d's agent lock: %w", n, err)
	}
	if ok {
		_ = a.Unlock()
	}
	return ok, nil
}

// TakeOwnSlot takes the run lock of slot n, the slot whose agent lock a
// live agent being adopted holds (liveslots.tla, Adopt). It needs no free
// slot and does not test the agent lock. An admission testing that slot
// holds its run lock only for a moment; TakeOwnSlot waits it out, polling.
// It gives up, returning nil, when live reports the agent gone, or with
// ctx's error when ctx ends.
func TakeOwnSlot(ctx context.Context, common string, n int, poll time.Duration, live func() bool) (*Slot, error) {
	if err := os.MkdirAll(SlotDir(common), 0o755); err != nil {
		return nil, err
	}
	for {
		f := flock.New(SlotLock(common, n))
		ok, err := f.TryLock()
		if err != nil {
			return nil, fmt.Errorf("locking slot %d: %w", n, err)
		}
		if ok {
			return &Slot{N: n, f: f}, nil
		}
		if !live() {
			return nil, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(poll):
		}
	}
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

// AgentHold is a held agent lock of a slot.
type AgentHold struct{ f *flock.Flock }

// ErrAgentLockHeld means another agent holds the slot's agent lock.
var ErrAgentLockHeld = errors.New("the slot's agent lock is held by another agent")

// HoldAgentLock takes slot n's agent lock (tuckerbox agent-hold,
// liveslots.tla Launch). It retries for wait, so a previous agent of the
// same run that is just exiting can let go, and then returns
// ErrAgentLockHeld.
func HoldAgentLock(common string, n int, wait time.Duration) (*AgentHold, error) {
	if err := os.MkdirAll(SlotDir(common), 0o755); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		f := flock.New(AgentLock(common, n))
		ok, err := f.TryLock()
		if err != nil {
			return nil, fmt.Errorf("locking slot %d's agent lock: %w", n, err)
		}
		if ok {
			return &AgentHold{f: f}, nil
		}
		if time.Now().After(deadline) {
			return nil, ErrAgentLockHeld
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Release frees the agent lock. The OS frees it too when the process ends.
func (h *AgentHold) Release() error {
	if h == nil || h.f == nil {
		return nil
	}
	err := h.f.Unlock()
	h.f = nil
	return err
}

// Queue orders the runs of one repository that wait for a slot. The
// queued run first in admission order (change-priority: Queue follows the
// order) may take a slot: lowest admission rank (priority, inherited from
// dependents), then earliest request, then id. With no priorities that is
// the order the runs were requested. The queue only orders: the slot locks
// alone bound how many runs run (WithinLimit).
//
// A waiting run has a ticket file <common>/tuckerbox/queue/<at>-<change>
// holding the path of its change lock; <at> is the request time. A ticket
// is live while that change lock is held; the run holds it for as long as
// it exists, so a run that dies leaves a dead ticket, which the next check
// removes.
//
// Publishing a ticket and an admission check (list the queue, read the
// priorities from the checkout, and, if first, try for a slot) each hold
// the sidecar lock, as does a priority move for all its writes, so no run
// is admitted on a stale or half-written order (formal/priority.tla).
// Removing a ticket needs no lock: it can only make a run first sooner.
type Queue struct {
	dir      string
	ticket   string
	change   string
	common   string
	checkout string
}

// QueueDir is the queue directory under the repository's common
// Tuckerbox directory.
func QueueDir(common string) string { return filepath.Join(common, "queue") }

// JoinQueue adds a ticket for change, requested at time at (nanoseconds),
// whose change lock (held by the caller) is at lockPath. checkout is the
// repository checkout whose tuckerbox.toml files hold the priorities (""
// for none). Old tickets of the same change are dead, since the caller
// holds its lock, and are removed. The ticket is published under the
// sidecar lock.
func JoinQueue(common, checkout, change, lockPath string, at int64) (*Queue, error) {
	dir := QueueDir(common)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	name := fmt.Sprintf("%020d-%s", at, change)
	err := WithSidecarLock(common, func() error {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if _, c, ok := parseTicket(e.Name()); ok && c == change {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
		tmp := filepath.Join(dir, "."+name+".tmp")
		if err := os.WriteFile(tmp, []byte(lockPath), 0o644); err != nil {
			return err
		}
		if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &Queue{dir: dir, ticket: name, change: change, common: common, checkout: checkout}, nil
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

// admitHook, when set (tests only), runs inside an admission check after
// the order is built and before the slot try.
var admitHook atomic.Pointer[func(change string, first bool)]

// first reports whether this ticket's run is first in admission order
// among the live tickets. It removes dead tickets. The caller holds the
// sidecar lock.
func (q *Queue) first() bool {
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		return true
	}
	var reqs []priority.Request
	mine := false
	for _, e := range entries {
		at, change, ok := parseTicket(e.Name())
		if !ok {
			continue
		}
		if e.Name() == q.ticket {
			mine = true
			reqs = append(reqs, priority.Request{ID: change, At: at})
			continue
		}
		p := filepath.Join(q.dir, e.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			continue // removed meanwhile
		}
		if !Active(strings.TrimSpace(string(data))) {
			_ = os.Remove(p)
			continue
		}
		reqs = append(reqs, priority.Request{ID: change, At: at})
	}
	if !mine || len(reqs) == 1 {
		return true
	}
	var all []priority.Change
	if q.checkout != "" {
		all, _ = priority.Load(q.checkout)
	}
	order := priority.Admission(reqs, all)
	return order[0].ID == q.change
}

// Head reports whether this ticket's run is first in admission order. It
// removes dead tickets.
func (q *Queue) Head() bool {
	ok := true
	if err := WithSidecarLock(q.common, func() error { ok = q.first(); return nil }); err != nil {
		return true
	}
	return ok
}

// TryAdmit is one admission check: under the sidecar lock, if this run is
// first in admission order, it tries for a free slot n < max. It returns
// nil when the run is not first or every slot is taken.
func (q *Queue) TryAdmit(max int) (*Slot, error) {
	var s *Slot
	err := WithSidecarLock(q.common, func() error {
		first := q.first()
		if h := admitHook.Load(); h != nil {
			(*h)(q.change, first)
		}
		if !first {
			return nil
		}
		var err error
		s, err = TrySlot(q.common, max)
		return err
	})
	return s, err
}

// Leave removes the ticket.
func (q *Queue) Leave() {
	if q != nil {
		_ = os.Remove(filepath.Join(q.dir, q.ticket))
	}
}

// WaitSlot waits, in admission order, for a free slot. It calls onWait once if
// it has to wait. It returns ctx's error if ctx ends first.
func WaitSlot(ctx context.Context, common string, max int, q *Queue, poll time.Duration, onWait func()) (*Slot, error) {
	waited := false
	for {
		var s *Slot
		var err error
		if q == nil {
			s, err = TrySlot(common, max)
		} else {
			s, err = q.TryAdmit(max)
		}
		if err != nil || s != nil {
			return s, err
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
