# Proposal

## Why

We run OpenSpec changes by hand, one agent session at a time, through the
`opsx:ship` workflow. We want to hand a repo's open changes to agents and have
them planned, built, reviewed and shipped, several at once. Each repo should
decide how its own work runs. The Herdr mods we reviewed (Kelpie, Muster,
Factory, Shepherdr) each cover part of this. All of them are tied to Herdr,
GitHub and their authors' own workflows, and none keeps its setup in the repo.
Tuckerbox is a standalone Go tool that does this on any machine, with or
without Herdr.

This first change builds the core: one machine, local only. A change goes
from "open in OpenSpec" to "built and reviewed in its own worktree", using the
steps the repo defines. Shipping, other machines, Herdr and parallel agents on
one change come in later changes (listed in design.md).

## What Changes

- A single cross-platform Go binary, `tuckerbox`, for macOS, Linux and Windows,
  with no runtime dependency on Herdr.
- A per-repo config file, `.tuckerbox/config.toml`, committed in the repo. It
  defines the workflow (steps, the agent and role for each step, review
  loops), concurrency and ship mode. Machine defaults live in the user's
  config directory. The repo file wins, except for agent permission flags,
  which only the machine can grant.
- `tuckerbox run <change>…` and `tuckerbox run --all` take OpenSpec changes
  through the workflow. Each change gets its own git worktree and branch. Up
  to `max_parallel` changes run at once.
- Agents are launched headless from command templates (Claude Code and Codex
  built in, any other CLI by template). Each step gets a written brief and
  reports with a small JSON result in its final message, which Tuckerbox
  records in a result file.
- Run state is kept per worktree, outside the committed tree. Runs can be
  stopped and resumed, and steps re-run.
- `tuckerbox status [--json]` shows every run on this machine. Later
  dashboards, including the Herdr panel, will read the JSON form.
- Ship mode in this change is `local` only: work stays committed on the
  change's branch and nothing is pushed.

## Capabilities

### New Capabilities

- `repo-config`: where workflow settings live, how repo and machine settings
  merge, validation, and which settings a repo may not set.
- `change-runs`: the lifecycle of one OpenSpec change through a workflow in its
  own worktree: steps, review loops, results, stop, resume, re-run, and the
  parallel limit.
- `agent-launch`: how a step starts an agent CLI, what the agent is told, how
  it reports, and how permissions are granted.
- `run-status`: the human and machine-readable view of runs on a machine.

### Modified Capabilities

None. This is the first change in a new repository.

## Impact

- New public repository on GitHub, Apache-2.0.
- Go 1.23+ toolchain. The `openspec` CLI must be installed where runs happen.
  Agent CLIs (`claude`, `codex`) must be installed and signed in on the
  machine running them.
- Agents can edit files in the change's worktree. Nothing is pushed.
