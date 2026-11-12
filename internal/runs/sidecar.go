package runs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// The sidecar lock, <common>/sidecar.lock, is one repository-wide OS file
// lock (change-priority: One writer for the sidecar). It is held:
//
//   - by every command that rewrites a change's tuckerbox.toml (priority,
//     approve, unapprove), around its read, change and write; a priority
//     move holds it until every file it changes is written;
//   - by JoinQueue, to publish a ticket;
//   - by a queued run's admission check, from listing the queue to its
//     try for a slot (formal/priority.tla, OrderedAdmission).
//
// Lock order is the change lock, then the sidecar lock, then a slot lock
// taken with a non-blocking try. The sidecar lock is held for milliseconds
// and nobody waits for anything else while holding it.

// SidecarLockPath is the sidecar lock under the repository's common
// Tuckerbox directory.
func SidecarLockPath(common string) string { return filepath.Join(common, "sidecar.lock") }

// SidecarLock is a held sidecar lock.
type SidecarLock struct{ f *flock.Flock }

// LockSidecar waits for and takes the sidecar lock.
func LockSidecar(common string) (*SidecarLock, error) {
	if err := os.MkdirAll(common, 0o755); err != nil {
		return nil, err
	}
	f := flock.New(SidecarLockPath(common))
	if err := f.Lock(); err != nil {
		return nil, fmt.Errorf("locking %s: %w", SidecarLockPath(common), err)
	}
	return &SidecarLock{f: f}, nil
}

// Release frees the lock. The OS frees it too if the process dies.
func (l *SidecarLock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Unlock()
	l.f = nil
	return err
}

// WithSidecarLock runs fn while holding the sidecar lock.
func WithSidecarLock(common string, fn func() error) error {
	l, err := LockSidecar(common)
	if err != nil {
		return err
	}
	defer l.Release()
	return fn()
}
