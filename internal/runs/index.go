package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/gofrs/flock"
)

// IndexVersion is the format version of the machine index.
const IndexVersion = 1

// Index is the machine index: every worktree that has had a run on this
// machine, so status finds runs across repositories without scanning disks.
type Index struct {
	Version int          `json:"version"`
	Runs    []IndexEntry `json:"runs"`
}

// IndexEntry is one known run.
type IndexEntry struct {
	Repo     string `json:"repo"`
	Change   string `json:"change"`
	Worktree string `json:"worktree"`
	// State is the worktree's Tuckerbox state directory.
	State string `json:"state"`
}

// UserStateDir returns the user's Tuckerbox state directory:
//
//   - $XDG_STATE_HOME/tuckerbox when XDG_STATE_HOME is an absolute path
//     (on any OS);
//   - otherwise %LocalAppData%\tuckerbox on Windows;
//   - otherwise ~/.local/state/tuckerbox on macOS, Linux and other Unix
//     systems.
func UserStateDir(getenv func(string) string, home func() (string, error), goos string) (string, error) {
	if x := getenv("XDG_STATE_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "tuckerbox"), nil
	}
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "windows" {
		if l := getenv("LocalAppData"); l != "" {
			return filepath.Join(l, "tuckerbox"), nil
		}
		return "", errors.New("finding the user state directory: %LocalAppData% is not set")
	}
	h, err := home()
	if err != nil {
		return "", fmt.Errorf("finding the home directory: %w", err)
	}
	return filepath.Join(h, ".local", "state", "tuckerbox"), nil
}

// SystemStateDir is UserStateDir for this process's environment.
func SystemStateDir() (string, error) {
	return UserStateDir(os.Getenv, os.UserHomeDir, runtime.GOOS)
}

// IndexPath is the index file in the user state directory dir.
func IndexPath(dir string) string { return filepath.Join(dir, "index.json") }

// ReadIndex reads the index in dir; a missing index is empty.
func ReadIndex(dir string) (*Index, error) {
	data, err := os.ReadFile(IndexPath(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return &Index{Version: IndexVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	var ix Index
	if err := json.Unmarshal(data, &ix); err != nil {
		return nil, fmt.Errorf("%s: %w", IndexPath(dir), err)
	}
	return &ix, nil
}

// Remember adds or updates e in the index in dir and drops entries whose
// state directory is gone (the worktree was removed). Writers are
// serialised by a lock next to the index.
func Remember(dir string, e IndexEntry) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	l := flock.New(filepath.Join(dir, "index.lock"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ok, err := l.TryLockContext(ctx, 20*time.Millisecond)
	if err != nil || !ok {
		return fmt.Errorf("locking the machine index: %v", err)
	}
	defer l.Unlock()
	ix, err := ReadIndex(dir)
	if err != nil {
		ix = &Index{} // a broken index is rebuilt
	}
	out := []IndexEntry{e}
	for _, o := range ix.Runs {
		if sameFile(o.State, e.State) {
			continue
		}
		if _, err := os.Stat(o.State); err != nil {
			continue
		}
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Repo != out[j].Repo {
			return out[i].Repo < out[j].Repo
		}
		return out[i].Change < out[j].Change
	})
	data, err := json.MarshalIndent(Index{Version: IndexVersion, Runs: out}, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(IndexPath(dir), append(data, '\n'))
}

func sameFile(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(fa, fb)
}
