package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Starter is the commented config that [WriteStarter] writes. It passes
// validation with no machine config.
const Starter = `# Tuckerbox repository config. Commit this file: every machine that checks
# out this revision runs the same workflow. Check it with: tuckerbox check
#
# Machine defaults live in config.toml in the tuckerbox folder of your user
# config directory. Values here win, except for machine-only settings:
# permission profiles, agent arguments and the editor.

# How many changes from this repository may run at once on one machine.
max_parallel = 1

# What happens when a run finishes: "local" leaves the work committed on the
# change's branch and pushes nothing.
ship = "local"

[workflow]
# The steps, in run order. Each needs a [steps.<id>] table below.
steps = ["plan", "build", "review"]

[steps.plan]
agent = "claude"        # built in: claude, codex; or define one under [agents]
role = "planner"        # built in: planner, builder, reviewer; or .tuckerbox/roles/<role>.md

[steps.build]
agent = "claude"
role = "builder"
# model = ""            # passed to the agent as {model}
# effort = ""           # passed to the agent as {effort}
# permissions = "edit"  # a profile that machine config must define and allow for this repo

[steps.review]
agent = "claude"
role = "reviewer"
on_reject = "build"     # on "rejected", go back to this step
max_rounds = 3          # then block the run instead of looping again

# Where runs put worktrees and what they call branches. {change} is the change
# id, {repo} this folder's name. A relative path is relative to this repo.
# [worktree]
# base = ""             # "" means the remote default branch
# path = "../{repo}-worktrees/{change}"
# branch = "{change}"

# A custom agent. The command gets {brief}, {workdir}, {model} and {effort}.
# Permission flags may not go here; they belong in machine config.
# [agents.myagent]
# command = ["myagent", "--prompt-file", "{brief}", "--cwd", "{workdir}"]
`

// ErrRepoConfigExists is returned by [WriteStarter] when the repository
// already has a config file.
var ErrRepoConfigExists = errors.New(filepath.ToSlash(RepoFile) + " already exists")

// WriteStarter writes [Starter] to root/.tuckerbox/config.toml and returns
// its path. It never overwrites an existing file.
func WriteStarter(root string) (string, error) {
	path := filepath.Join(root, RepoFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return path, fmt.Errorf("%s: %w", path, ErrRepoConfigExists)
	}
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(Starter); err != nil {
		f.Close()
		return "", err
	}
	return path, f.Close()
}
