package approval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// ChangeDir is a change's folder relative to a repository root,
// slash-separated.
func ChangeDir(change string) string { return "openspec/changes/" + change }

// Dir is a change folder on disk.
type Dir string

// DirOf returns the folder of change in the checkout or worktree at root.
func DirOf(root, change string) Dir {
	return Dir(filepath.Join(root, filepath.FromSlash(ChangeDir(change))))
}

// Read implements [Files].
func (d Dir) Read(rel string) ([]byte, error) {
	return os.ReadFile(filepath.Join(string(d), filepath.FromSlash(rel)))
}

// Specs implements [Files].
func (d Dir) Specs() ([]string, error) {
	root := filepath.Join(string(d), "specs")
	var out []string
	err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == root {
				return fs.SkipAll
			}
			return err
		}
		if e.Type().IsRegular() {
			rel, err := filepath.Rel(string(d), p)
			if err != nil {
				return err
			}
			if rel = filepath.ToSlash(rel); IsSpec(rel) {
				out = append(out, rel)
			}
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// SidecarPath is the change's tuckerbox.toml.
func (d Dir) SidecarPath() string { return filepath.Join(string(d), SidecarName) }

// Rev is a change folder in a git revision of the repository at Repo, such
// as the base branch a new worktree would come from.
type Rev struct {
	Repo, Rev, Change string

	files  map[string]bool
}

// NewRev reads which files change has at rev. A change that is not there
// has no files: every Read reports fs.ErrNotExist.
func NewRev(ctx context.Context, repo, rev, change string) (*Rev, error) {
	r := &Rev{Repo: repo, Rev: rev, Change: change, files: map[string]bool{}}
	out, err := gitOut(ctx, repo, "ls-tree", "-r", "-z", "--name-only", rev, "--", ChangeDir(change)+"/")
	if err != nil {
		return nil, err
	}
	prefix := ChangeDir(change) + "/"
	for _, name := range strings.Split(string(out), "\x00") {
		if rel, ok := strings.CutPrefix(name, prefix); ok && rel != "" {
			r.files[rel] = true
		}
	}
	return r, nil
}

// Exists reports whether the change has any file at the revision.
func (r *Rev) Exists() bool { return len(r.files) > 0 }

// Read implements [Files].
func (r *Rev) Read(rel string) ([]byte, error) {
	if !r.files[rel] {
		return nil, fmt.Errorf("%s:%s/%s: %w", r.Rev, ChangeDir(r.Change), rel, fs.ErrNotExist)
	}
	// cat-file gives the blob's bytes as committed, with no filters.
	return gitOut(context.Background(), r.Repo, "cat-file", "blob", r.Rev+":"+path.Join(ChangeDir(r.Change), rel))
}

// Specs implements [Files].
func (r *Rev) Specs() ([]string, error) {
	var out []string
	for f := range r.files {
		if IsSpec(f) {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out, nil
}

func gitOut(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out, nil
}
