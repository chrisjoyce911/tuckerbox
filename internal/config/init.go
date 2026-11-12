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
# Machine defaults live in $XDG_CONFIG_HOME/tuckerbox/config.toml, else
# ~/.config/tuckerbox/config.toml on macOS and Linux, or
# %AppData%\tuckerbox\config.toml on Windows. Values here win, except for
# machine-only settings: permission profiles, agent arguments and the editor.

# How many changes from this repository may run at once on one machine
# (default 1).
max_parallel = 1

# What happens when a run finishes: "local" leaves the work committed on the
# change's branch and pushes nothing.
ship = "local"

# Board layout: "compact" (default) groups queued runs and workflow steps.
# Use "steps" for a Queued column and one column per workflow step.
# [board]
# columns = "compact"

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
# permissions = "edit"  # a machine profile that allows this repo's origin remote
#                       # (or "*") and grants flags for this step's agent

[steps.review]
agent = "claude"
role = "reviewer"
on_reject = "build"     # on "rejected", go back to this step (this step or an earlier one)
max_rounds = 3          # then block the run (default 3; needs on_reject)

# Every [steps.<id>] table must be listed in workflow.steps; one that is not
# can never run, and tuckerbox check reports it.

# Where runs put worktrees and what they call branches. {change} is the change
# id, {repo} this folder's name. A relative path is relative to this repo.
# These are the defaults.
# [worktree]
# base = ""             # "" means the remote default branch (local default if no remote)
# path = "../{repo}-worktrees/{change}"
# branch = "{change}"

# A custom agent. The command must pass {brief} and may use {workdir},
# {model}, {effort} and {lastmessage}. The agent reports by ending with the
# result JSON as its final message: Tuckerbox reads it from {lastmessage}, a
# file the CLI writes the final message to, or else from its standard output. The built-in agents (claude, codex) cannot be
# redefined here, and permission flags (such as --dangerously-skip-permissions,
# --permission-mode or --sandbox) may not go in the command: they belong in a
# permission profile in machine config.
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
