# Proposal

## Why

A run takes a whole change through build and review in one go. A large
change then gives the builder too much at once, and the reviewer a large
diff. The owner wants to run one task group, check it, and then run the
next, in the same worktree and on the same branch.

Kelpie gets small steps by giving each task its own worktree. We keep one
run per change (`formal/runs.tla`), because a change's tasks share one
branch, one approval and one review. So a scope limits what a run works on,
not where it works.

This is the first change Tuckerbox will build through itself, so it is kept
small.

## What Changes

- `tuckerbox run <change> --tasks <scope>` runs part of a change. A scope is
  a task group (`2`), one task (`2.3`), or a range of tasks in tasks.md order
  (`2.1-2.4`). `--tasks next` picks the first group with an unticked task,
  so a person or `add-autopilot` can work through a change group by group
  without naming groups. It takes one change, not `--all`.
- The scope belongs to the change's one run. It is kept in `run.json`, and
  `resume` and `rerun` keep it. When the run is ready, stopped, blocked or
  failed, `run --tasks` with a different scope starts the run again from
  its first step with the new scope. Rounds keep counting up.
- The brief lists the scope's tasks and tells every step to work on, tick
  and judge only those tasks. So the builder does only the scope, and the
  reviewer reviews only the scope.
- A scoped run is `ready` only when every task in its scope is ticked. If
  not, it is `blocked` and names the unticked tasks.
- Approval still covers the whole change. The run gate is unchanged, and
  starting the next scope is a start, so it is gated again.
- A dependency counts as ready only once all its tasks are ticked, so one
  finished group doesn't release the changes that depend on it.
- `status`, history and board cards show the scope. A new history event,
  `scope_started`, marks a run starting again for a new scope.
- The board's card detail gets **Run group…**: pick a group with unticked
  tasks, and the board runs `tuckerbox run <change> --tasks <n>`.

## Capabilities

### New Capabilities

- `task-scope`: running part of a change: choosing the scope, how it fits
  one run per change and approval, when a scoped run is ready, and where the
  scope shows.

### Modified Capabilities

- `agent-launch`: the step brief also holds the run's task scope.

The board, history and status additions are in `task-scope`, because
`add-herdr-board` (board data, history, the mod) isn't archived yet. See
design.md.

## Impact

- Depends on `add-herdr-board` (history, board data, the mod and the run
  gate).
- Run engine, brief, `status`, `history` and `board` code, and the board
  mod. No new config keys and no new state files.
- `run.json` gains a `scope` field. Runs without one behave as today.
- No new concurrency: a scope is a field of the one run a change already
  has. See model.md.
