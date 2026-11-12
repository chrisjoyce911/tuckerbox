package config

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Env is what [Discover] needs from the outside world, so tests can replace it.
type Env struct {
	// Git runs git with args in dir and returns its standard output.
	Git func(dir string, args ...string) (string, error)
	// UserConfigDir returns the user's config directory, as
	// os.UserConfigDir does.
	UserConfigDir func() (string, error)
}

// SystemEnv returns an Env that runs the real git and uses os.UserConfigDir.
func SystemEnv() Env {
	return Env{Git: runGit, UserConfigDir: os.UserConfigDir}
}

func runGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// MachinePath returns the machine config file:
// os.UserConfigDir()/tuckerbox/config.toml.
func MachinePath(env Env) (string, error) {
	dir, err := env.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding the user config directory: %w", err)
	}
	return filepath.Join(dir, "tuckerbox", "config.toml"), nil
}

// RepoRoot returns the root of the git checkout (repository or worktree)
// that contains dir.
func RepoRoot(env Env, dir string) (string, error) {
	out, err := env.Git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s is not in a git repository: %w", dir, err)
	}
	return filepath.Clean(filepath.FromSlash(strings.TrimSpace(out))), nil
}

// Remote returns the URL of the repository's remote that permission
// profiles are matched against: origin, or the only remote when there is
// no origin. It returns "" when there is no such remote.
func Remote(env Env, root string) string {
	if out, err := env.Git(root, "config", "--get", "remote.origin.url"); err == nil {
		if u := strings.TrimSpace(out); u != "" {
			return u
		}
	}
	out, err := env.Git(root, "remote")
	if err != nil {
		return ""
	}
	names := strings.Fields(out)
	if len(names) != 1 {
		return ""
	}
	out, err = env.Git(root, "config", "--get", "remote."+names[0]+".url")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Discover returns the [Options] for the checkout that contains dir: its
// root, its remote and the machine config path.
func Discover(env Env, dir string) (Options, error) {
	root, err := RepoRoot(env, dir)
	if err != nil {
		return Options{}, err
	}
	machine, err := MachinePath(env)
	if err != nil {
		return Options{}, err
	}
	return Options{RepoRoot: root, MachinePath: machine, Remote: Remote(env, root)}, nil
}
