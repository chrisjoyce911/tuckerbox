// Package fleetslot is machine-wide admission for dispatched runs: a
// machine with fleet_capacity = n has n fleet slots, OS file locks in the
// user state directory shared by every repository on the machine. A run
// started by dispatch (tuckerbox run --dispatched) takes one before it
// claims its change and holds it while queued, running or shipping
// (formal/runs.tla, WithinLimit). A run a person starts takes none.
//
// The lock is held by the process that holds it, so a run that crashes
// frees its slot.
package fleetslot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gofrs/flock"
)

// ErrFull is wrapped by [Take]'s error when every fleet slot is taken (or
// the machine takes no fleet runs). Its message is "<machine> is full".
var ErrFull = errors.New("full")

// FullError is the error [Take] returns when no slot is free.
type FullError struct{ Machine string }

func (e *FullError) Error() string { return e.Machine + " is full" }

// Is reports ErrFull.
func (e *FullError) Is(target error) bool { return target == ErrFull }

// Dir is the fleet slot directory in the user state directory stateHome
// (see runs.UserStateDir).
func Dir(stateHome string) string { return filepath.Join(stateHome, "fleet-slots") }

func slotPath(stateHome string, n int) string {
	return filepath.Join(Dir(stateHome), strconv.Itoa(n)+".lock")
}

// admitLock serialises takes and counts, so that a count testing a free
// slot never makes a take at the same moment find it busy.
func admitLock(ctx context.Context, stateHome string) (*flock.Flock, error) {
	if err := os.MkdirAll(Dir(stateHome), 0o755); err != nil {
		return nil, err
	}
	f := flock.New(filepath.Join(Dir(stateHome), "admit.lock"))
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ok, err := f.TryLockContext(ctx, 10*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("locking the fleet slots: %w", err)
	}
	if !ok {
		return nil, errors.New("locking the fleet slots: timed out")
	}
	return f, nil
}

// Slot is a held fleet slot.
type Slot struct {
	N int
	f *flock.Flock
}

// Take takes a free fleet slot n < capacity, or returns a [FullError]
// naming machine when all are held.
func Take(stateHome, machine string, capacity int) (*Slot, error) {
	if capacity <= 0 {
		return nil, &FullError{Machine: machine}
	}
	admit, err := admitLock(context.Background(), stateHome)
	if err != nil {
		return nil, err
	}
	defer func() { _ = admit.Unlock() }()
	for n := 0; n < capacity; n++ {
		f := flock.New(slotPath(stateHome, n))
		ok, err := f.TryLock()
		if err != nil {
			return nil, fmt.Errorf("locking fleet slot %d: %w", n, err)
		}
		if ok {
			return &Slot{N: n, f: f}, nil
		}
	}
	return nil, &FullError{Machine: machine}
}

// InUse counts the fleet slots n < capacity held now, by any process.
func InUse(stateHome string, capacity int) (int, error) {
	if capacity <= 0 {
		return 0, nil
	}
	admit, err := admitLock(context.Background(), stateHome)
	if err != nil {
		return 0, err
	}
	defer func() { _ = admit.Unlock() }()
	used := 0
	for n := 0; n < capacity; n++ {
		f := flock.New(slotPath(stateHome, n))
		ok, err := f.TryLock()
		if err != nil {
			return 0, fmt.Errorf("locking fleet slot %d: %w", n, err)
		}
		if ok {
			_ = f.Unlock()
		} else {
			used++
		}
	}
	return used, nil
}

// Free gives the slot back. Freeing a nil or freed slot does nothing.
func (s *Slot) Free() error {
	if s == nil || s.f == nil {
		return nil
	}
	err := s.f.Unlock()
	s.f = nil
	return err
}
