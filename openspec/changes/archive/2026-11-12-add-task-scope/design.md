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
  `add-autopilot` (proposed) does that with `--tasks next`. A person
  running groups by
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
A dependent waits for the whole change. Its check must not combine the
dependency's run state with its live tasks.md. Those are two reads, and A
can move between them: B reads A as ready after scope 1, A starts scope 2
and its builder ticks the rest, and B reads a fully ticked tasks.md. B
would start on work no review has passed, and at no moment was A both
ready and complete. (`formal/scope.tla`, broken variant
`scope_broken_split`.)

So completeness is recorded, not read:
- When a scoped run becomes `ready`, the engine checks whether every task
  of the change is ticked and writes `complete: true|false` into
  `run.json` in the same write that sets `ready`. It holds A's change lock,
  and `run.json` is replaced with a rename, so a reader sees both or
  neither.
- Starting another scope writes `queued` and clears `complete` in one
  write.
- A dependent reads A's `run.json` once: met when `ready` and `complete`
  (or `ready` with no scope, as today). It never reads A's tasks.md.
- Ticks by hand (`tuckerbox tasks`) after a partial run don't release
  dependents. The rest of the change has to run and pass review.

The queued reason shows the scope, for example `waiting on A (ready,
tasks 1)`. The existing check again after taking a slot stays as it is.

In `mr` and `merge` modes, `add-forge-shipping` already says a dependency
counts only once archived on the base branch. That rule wins: ticks and
`complete` never release dependents there.

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
`add-forge-shipping` ships a run when its last step reports `done`, and
shipping starts by archiving the change. A run for group 1 of 3 must never
do that, so the rule is in this change, not left to a later one:
- In `mr` or `merge` mode, when the last step reports `done`, the engine
  records `complete` as above. If it is false, the run becomes `ready` and
  nothing is archived, pushed, opened or merged. If it is true, the run
  enters `shipping`.
- `add-forge-shipping` now depends on this change, and its `Ship modes`
  requirement defers to this rule. Its tasks include the failure tests
  (partial run in `merge` mode archives nothing, pushes nothing, opens no
  merge request).
- This change is built first. Until forge shipping exists, `ship` accepts
  only `local`, so no scoped run can ship. The engine's decision ("ready,
  or go on to ship?") is a function of `complete` and the ship mode, and is
  unit tested here.

### Ship tasks
Most tasks.md files end with a task such as "Archive this change once
accepted". No step can honestly tick it before shipping, and shipping only
starts once the change is complete. Without a rule, the last group could
never ship. So a task whose text starts with `(ship)` is a ship task:
- it is never in a scope or `next`, and doesn't count toward `complete`
- the brief tells steps not to tick it
- in `mr` and `merge` modes, the ship stage ticks it in the archive commit,
  the commit that does the archiving
- in `local` mode it stays for the person, as today

A marker in the text was chosen over a heading or a sidecar list: it
travels with the task, and OpenSpec ignores it. Ticks don't change the
approval hash, so the archive commit's tick keeps the approval.

### Whole-change runs that leave tasks unticked
A run without a scope keeps today's rule in `local` mode: ready when the
last step is done. In `mr` and `merge` modes, an incomplete change must not
ship, and a `ready` run would just be started again and refused, pass
after pass. So it becomes `blocked`, naming the unticked tasks: a state a
person sees and `add-autopilot` reports.

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
