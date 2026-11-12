# Design

## Context

- A change has one run record, `run.json` in its worktree's git dir. The run
  lock makes sure only one process uses it (`formal/runs.tla`,
  `OneRunPerChange`).
- `tuckerbox run` on a `ready` run refuses today. `rerun` starts from a named
  step, and each new attempt raises the round, so a late result from an
  earlier attempt is never accepted (`StaleNeverAccepted`).
- tasks.md uses `## N. Title` group headings and `- [ ] N.M text` tasks.
  `add-herdr-board` already parses task numbers for the board and for
  `tuckerbox tasks tick`.
- The approval hash reads every checkbox as unchecked, so ticking keeps
  approval.

## Goals / Non-Goals

**Goals:**
- Run one group, or a few tasks, of a change, then the next, on one branch.
- Keep the builder and the reviewer inside the scope.
- No new store, lock, config key or kind of concurrency.

**Non-Goals:**
- **Scopes in parallel.** Two groups of one change never run at once. They
  share a worktree, a branch and tasks.md.
- **Advancing by itself.** A ready scoped run doesn't start the next group.
  `add-autopilot` does that with `--tasks next`. A person running groups by
  hand wants to look in between.
- **Checking which files a step touched.** The brief and the reviewer keep
  the work in scope. Tuckerbox checks only the ticks.
- **Lists of scopes** such as `1,3`. One group, task or range is enough to
  start with.

## Decisions

### A scope is a field of the one run
`run.json` gains `scope`: what the user typed (after `next` is resolved) and
the task numbers it covers. No second run record, so `status`, `stop`,
`logs` and the locks work as they do now.

Starting the next scope reuses the run, like `rerun` from the first step:
- the round goes up, so `StaleNeverAccepted` still holds
- rejections are cleared, so each scope gets `max_rounds` of its own
- the worktree, branch and earlier commits stay

Alternative considered: a new run record per scope. Rejected: it adds a
second record per change, and the history and rounds would have to be
joined again for the board.

### Resolving the scope
The scope is resolved against the tasks.md the run will use, the same place
the run gate reads (worktree, else the commit the worktree will be made
from). `next` becomes a group number then, and only the number is stored.
So history and the board show what ran, and a later tick doesn't change
the meaning of a stored scope.

A range runs in document order and may cross groups. A range whose start is
after its end, or a number tasks.md doesn't have, is refused. A scope whose
tasks are all ticked is refused, and so is `next` when every task is ticked.

### When a scoped run is ready
Today a run is ready when its last step reports `done`. A scoped run also
needs every task in its scope ticked in the worktree's tasks.md. If one is
not, the run is `blocked` with the unticked numbers, and `resume` re-runs
the last step. This is the only check Tuckerbox adds. It catches a builder
that forgot to tick, or a reviewer that passed undone work.

A run without a scope keeps today's rule. Making whole-change runs check
every tick too is an open question for the owner.

### Approval
Approval stays on the whole change. A scope is how much of an approved plan
runs now, not a smaller plan. Starting the next scope is a start, so the
run gate runs again. If a builder edited the design during scope 1, the
change must be approved again before scope 2. That is the point at which
the owner wants to see the change.

### Dependencies
A dependent waits for the whole change. A ready run with a scope counts as
a met dependency only once all of its tasks are ticked. The queued reason
shows the scope, for example `waiting on A (ready, tasks 1)`.

### Brief
The brief gets a section after the change files:

```
## Scope

This run covers only these tasks:
- 2.1 Parse --tasks
- 2.2 Refuse unknown numbers

Work on, tick and judge only these tasks. Tasks outside the scope are not
part of this run: don't do them, don't tick them, and don't reject the
change because they are undone. Work or ticks outside the scope are a
finding.
```

The same text goes to every step. The role prompts don't change.

### History and board
- History events `started`, `resumed`, `rerun`, `ready` and `blocked` gain
  a `scope` field. A new `scope_started` event (old and new scope) comes
  before `queued` when the run starts again for a new scope. Readers already
  ignore kinds they don't know.
- Board cards gain `scope`. Rounds in card detail gain the scope they ran
  with. Card detail gains `groups`, each with number, title, ticked and
  total counts, so the mod can build its menu without parsing tasks.md.
- **Run group…** is a menu in card detail. It is offered where Start is, and
  on Ready cards that still have unticked tasks. It lists only groups with
  unticked tasks.

### Where the requirements live
`add-herdr-board` isn't archived, so its `board-data`, `run-history` and
`herdr-board` specs can't take deltas yet. The board, history and mod
requirements stay in `task-scope`, as `add-forge-shipping` does with
`ship-board`. When both are archived they fold into those capabilities.
`agent-launch` is archived, so its `Step brief` gets a MODIFIED delta.

### Shipping
`add-forge-shipping` isn't built yet. A scoped run that reaches the last
step in `mr` or `merge` mode must not archive and ship a half-done change.
`add-autopilot` adds that rule ("partial runs don't ship"), because it
depends on both changes. Until forge shipping exists, every run ends at
`ready` in local mode, so nothing ships early.

## Risks / Trade-offs

- **Task numbers move.** If a step renumbers tasks.md, the stored numbers
  may point at other tasks. The readiness check names a missing number, and
  the reviewer sees the brief's task text. Planning steps should not
  renumber tasks in a scoped run.
- **The scope is guidance, not a fence.** A builder can still change code
  for other tasks. The reviewer is told to report it, and the tick check
  catches ticks outside the scope only through review. A file-level fence
  would need a task-to-file map, which OpenSpec doesn't have.
- **More rounds per change.** Each scope runs build and review again. That
  costs more agent time than one big run, in exchange for smaller reviews.
