package configsave

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// Revision is the hash used by config show and --if-match. Absent files
// have no revision; an empty existing file has the hash of empty bytes.
func Revision(data []byte) string { s := sha256.Sum256(data); return hex.EncodeToString(s[:]) }

// WithFileLock serialises readers/checkers/writers across processes. Lock
// files stay in the machine state directory, never alongside repository files.
func WithFileLock(ctx context.Context, stateDir, path string, fn func() error) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	// Resolve the nearest existing ancestor, including on the first save
	// when several parent folders do not exist yet. Creating those folders
	// must not change the lock name for a later writer.
	parent := filepath.Dir(abs)
	missing := []string{filepath.Base(abs)}
	for {
		if resolved, e := filepath.EvalSymlinks(parent); e == nil {
			abs = resolved
			for i := len(missing) - 1; i >= 0; i-- {
				abs = filepath.Join(abs, missing[i])
			}
			break
		}
		next := filepath.Dir(parent)
		if next == parent {
			break
		}
		missing = append(missing, filepath.Base(parent))
		parent = next
	}
	dir := filepath.Join(stateDir, "config-locks")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(dir, Revision([]byte(abs))+".lock"))
	ok, err := lock.TryLockContext(ctx, 25*time.Millisecond)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("config file lock: %w", ctx.Err())
	}
	defer lock.Unlock()
	return fn()
}

// AtomicWrite writes in the destination directory, preserves the requested
// permissions and renames only a completely written, synced file.
func AtomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-save-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode.Perm()); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
