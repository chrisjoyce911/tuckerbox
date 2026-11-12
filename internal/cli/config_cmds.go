package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/deps"
	"github.com/chrisjoyce911/tuckerbox/internal/priority"
)

func (a App) configEnv() (config.Env, func() (string, error)) {
	env := a.ConfigEnv
	sys := config.SystemEnv()
	if env.Git == nil {
		env.Git = sys.Git
	}
	if env.Getenv == nil {
		env.Getenv = sys.Getenv
	}
	if env.HomeDir == nil {
		env.HomeDir = sys.HomeDir
	}
	if env.UserConfigDir == nil {
		env.UserConfigDir = sys.UserConfigDir
	}
	if env.GOOS == "" {
		env.GOOS = sys.GOOS
	}
	getwd := a.Getwd
	if getwd == nil {
		getwd = os.Getwd
	}
	return env, getwd
}

// check validates the merged config of the repository in the working
// directory and prints every problem with its file and key.
func (a App) check() int {
	env, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		fmt.Fprintln(a.Stderr, "tuckerbox check:", err)
		return 1
	}
	opts, err := config.Discover(env, wd)
	if err != nil {
		fmt.Fprintln(a.Stderr, "tuckerbox check:", err)
		return 1
	}
	cfg, err := config.Load(opts)
	depProblems, depErr := deps.Check(opts.RepoRoot)
	if depErr != nil {
		fmt.Fprintln(a.Stderr, "tuckerbox check: dependencies:", depErr)
	}
	prioProblems, prioErr := priority.Check(opts.RepoRoot)
	if prioErr != nil {
		fmt.Fprintln(a.Stderr, "tuckerbox check: priority:", prioErr)
		depErr = errors.Join(depErr, prioErr)
	}
	var problems config.Problems
	if err != nil && !errors.As(err, &problems) {
		fmt.Fprintln(a.Stderr, "tuckerbox check:", err)
		return 1
	}
	if n := len(problems) + len(depProblems) + len(prioProblems); n > 0 {
		for _, p := range problems {
			p.File = displayPath(opts.RepoRoot, p.File)
			fmt.Fprintln(a.Stderr, p)
		}
		for _, p := range depProblems {
			fmt.Fprintln(a.Stderr, p)
		}
		for _, p := range prioProblems {
			fmt.Fprintln(a.Stderr, p)
		}
		noun := "problems"
		if n == 1 {
			noun = "problem"
		}
		fmt.Fprintf(a.Stderr, "tuckerbox check: %d %s\n", n, noun)
		return 1
	}
	if depErr != nil {
		return 1
	}

	machine := "none"
	if cfg.MachinePath != "" {
		machine = cfg.MachinePath
	}
	ids := make([]string, len(cfg.Steps))
	for i, s := range cfg.Steps {
		ids[i] = s.ID
	}
	fmt.Fprintf(a.Stdout, "config ok: %s (machine config: %s)\n", displayPath(opts.RepoRoot, cfg.RepoPath), machine)
	fmt.Fprintf(a.Stdout, "workflow: %s; max_parallel %d; ship %s\n", strings.Join(ids, " -> "), cfg.MaxParallel, cfg.Ship)
	return 0
}

// initConfig writes a commented starter config at the repository root.
func (a App) initConfig() int {
	env, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		fmt.Fprintln(a.Stderr, "tuckerbox init:", err)
		return 1
	}
	root, err := config.RepoRoot(env, wd)
	if err != nil {
		fmt.Fprintln(a.Stderr, "tuckerbox init:", err)
		return 1
	}
	path, err := config.WriteStarter(root)
	if err != nil {
		if errors.Is(err, config.ErrRepoConfigExists) {
			fmt.Fprintf(a.Stderr, "tuckerbox init: %s already exists; not changed\n", displayPath(root, path))
		} else {
			fmt.Fprintln(a.Stderr, "tuckerbox init:", err)
		}
		return 1
	}
	fmt.Fprintf(a.Stdout, "wrote %s; edit it, run tuckerbox check, then commit it\n", displayPath(root, path))
	return 0
}

// displayPath shows files inside the repository relative to its root.
func displayPath(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return path
}
