// Package statedir finds where Tuckerbox keeps a worktree's run state:
// $(git rev-parse --git-dir)/tuckerbox. The directory is per worktree, is never
// committed and is removed with the worktree.
package statedir

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Name is the directory Tuckerbox uses inside a git directory.
const Name = "tuckerbox"

// Dir is a worktree's Tuckerbox state directory.
type Dir string

// Resolve returns the state directory of the worktree that contains dir.
// It does not create it.
func Resolve(ctx context.Context, dir string) (Dir, error) {
	gitDir, err := gitPath(ctx, dir, "--git-dir")
	if err != nil {
		return "", err
	}
	return Dir(filepath.Join(gitDir, Name)), nil
}

// ResolveCommon returns the Tuckerbox directory in the main repository's git
// common directory, shared by all worktrees of the repository (for example
// for slot locks). It does not create it.
func ResolveCommon(ctx context.Context, dir string) (string, error) {
	common, err := gitPath(ctx, dir, "--git-common-dir")
	if err != nil {
		return "", err
	}
	return filepath.Join(common, Name), nil
}

// gitPath runs `git rev-parse <flag>` in dir and returns the absolute result.
func gitPath(ctx context.Context, dir, flag string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", flag)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git rev-parse %s in %s: %s", flag, dir, msg)
	}
	p := strings.TrimSpace(string(out))
	if p == "" {
		return "", fmt.Errorf("git rev-parse %s in %s: empty output", flag, dir)
	}
	p = filepath.FromSlash(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return abs, nil
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// CheckID reports whether id (a step id) is safe to use in a file name.
func CheckID(kind, id string) error {
	if !idPattern.MatchString(id) || strings.Contains(id, "..") {
		return fmt.Errorf("%s %q: use letters, digits, '.', '_' and '-' only", kind, id)
	}
	return nil
}

// Ensure creates the state directory and its subdirectories.
func (d Dir) Ensure() error {
	for _, sub := range []string{"briefs", "results", "logs"} {
		if err := os.MkdirAll(filepath.Join(string(d), sub), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func stepRound(step string, round int) string {
	return step + "-" + strconv.Itoa(round)
}

// Brief is the brief file for a step and round.
func (d Dir) Brief(step string, round int) string {
	return filepath.Join(string(d), "briefs", stepRound(step, round)+".md")
}

// Result is the result file for a step and round.
func (d Dir) Result(step string, round int) string {
	return filepath.Join(string(d), "results", stepRound(step, round)+".json")
}

// Log is the log file of a step. All rounds of a step append to it.
func (d Dir) Log(step string) string {
	return filepath.Join(string(d), "logs", step+".log")
}

// RunFile is the run state file, written only by the engine.
func (d Dir) RunFile() string { return filepath.Join(string(d), "run.json") }

// RunLock is the change lock file.
func (d Dir) RunLock() string { return filepath.Join(string(d), "run.lock") }

// PidFile holds the pid of the run process.
func (d Dir) PidFile() string { return filepath.Join(string(d), "run.pid") }
