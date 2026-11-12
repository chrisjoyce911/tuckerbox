# Design

## Context

- The slot queue (`internal/runs`) gives each waiting run a ticket file
  named `<time>-<change>` in the repository's shared `tuckerbox/queue/`
  folder. Only the oldest live ticket may try for a slot. The slot locks
  alone bound how many run (`WithinLimit`), so the queue only orders.
- A run waiting on dependencies holds no ticket and no slot
  (`add-herdr-board`).
- `openspec/changes/<id>/tuckerbox.toml` already holds `title`,
  `depends_on` and `[approval]`. `add-change-settings` (proposed) adds
  `[steps.*]` and `machines`. The approval hash covers proposal, design,
  model, tasks and spec deltas, not `tuckerbox.toml`.
- The board has compact (Proposed, Approved, In progress, Ready, Done) and
  steps layouts.

## Goals / Non-Goals

**Goals:**
- One order the owner sets, used by the board, `run --all`, the queue and
  `add-autopilot`.
- Reordering by drag, with the CLI as the only writer.
- Priority edits never touch approval and never wait for a run.

**Non-Goals:**
- **Preemption.** A higher priority never stops or pauses a running change.
  It only decides who takes the next free slot.
- **Fairness for low ranks.** A change at the bottom can wait as long as
  higher ones keep coming. That is what priority means here.
- **Priority across repositories.** Each repository has its own order and
  its own `max_parallel`.
- **Moving cards between columns.** Columns stay derived.

## Decisions

### An ordinal rank, not a 1 to 5 scale
`priority` is a rank: lower first, and every change has its own place.

- **Drag needs a total order.** Dropping a card between two others has to
  put it exactly there. With five levels, most cards share a level, so a
  drop within a level changes nothing, or has to change the level of the
  cards around it.
- **Levels drift.** Everything urgent becomes 1, and the scale stops saying
  what goes next. A rank always answers "what next", which is the queue's
  question.
- **Ties still exist, and are harmless.** Two machines can give two changes
  the same number. Age, then id, breaks the tie, so the order is always
  total and the same everywhere.

The cost: the stored number means nothing on its own. The CLI and the
board show positions (1, 2, 3), and people use positions. Only Tuckerbox
chooses the numbers.

### Sparse numbers, so a move writes one file
Tuckerbox stores numbers with gaps (1000, 2000, …) and gives a moved change
a number between its new neighbours. A move then rewrites one file, so:
- a commit after a drag has a one-line diff
- two people reordering on two machines rarely touch the same file, and
  if they do, git shows one conflicting line

Dense positions (1, 2, 3) were rejected: moving one change to the top would
rewrite every file below it, and concurrent reorders would always conflict.
Fractional keys (LexoRank) were rejected as opaque in a file people read.

When no integer gap is left, Tuckerbox renumbers the ranked changes to
1000, 2000, … in their current order. Moving a change down into the
unranked part first ranks the unranked changes above it, in their current
order, because an unranked change always comes after a ranked one.

### Read and write in the checkout
Priority is about every change at once, so one copy of all the values must
decide the order. That copy is the repository checkout on this machine:
- every active change has its folder there, worktree or not
- it's where the owner works, and a drag must count at once, without a
  commit, for the next free slot
- a change's worktree holds an old copy of the other changes' files, made
  when it branched

So `tuckerbox priority` writes the checkout, even for a change that has a
worktree. That is unlike `approve`, which writes where the run will read.
The change's branch never changes its own `priority`, so when it merges,
git merges the base's priority line cleanly.

Uncommitted priorities count on this machine. Other machines see them once
committed and pulled. A multi-machine board takes its order from the
machine it runs on.

### Approval
The hash covers only proposal.md, design.md, model.md, tasks.md and
`specs/**/spec.md` (`add-herdr-board`, `Approval hash`). `tuckerbox.toml`
is not hashed, since it holds the hash. So a `priority` line can't change
the hash by construction, and no special rule is needed. Two guards keep it
so:
- `priority` rewrites only its own line. A new key is inserted before the
  first table, as TOML requires.
- `approve` and `unapprove` already keep other keys. A test pins that they
  keep `priority`.

A priority change also doesn't take the change's run lock, unlike
`approve`: it must work while the change is queued, which is when
reordering matters.

### One lock for sidecar writes
`approve` on a change without a worktree writes the checkout's sidecar with
no lock, because no run can use the checkout. With `priority` (and
`add-change-settings`' `set`), two commands can now rewrite the same file
at once, and one would lose the other's edit. Every sidecar writer takes
`<common-dir>/tuckerbox/sidecar.lock`, an OS file lock, around read, change
and write. It writes a temporary file and renames it. The lock is held for
milliseconds, and it is always taken last (after a change's run lock, if
any), so lock order can't deadlock.

### Admission order
One sort key, used by `run --all`, the queue and `add-autopilot`:

1. **effective priority:** the first, in priority order, of the change and
   every active change that depends on it, directly or not
2. a dependency before its dependent
3. **first in, first out:** the queue ticket's time (for `--all`, age)
4. change id

Inheriting the rank is what "dependencies first" means in practice. A
dependent can't run before its dependency anyway (it waits without a
ticket). Without inheritance, a top-priority change could wait behind
everything ranked above its low-priority dependency.

Effective priority is never above the dependent's, so the order is always
a valid dependency order.

### The queue reads priority live
Tickets keep their names (`<time>-<change>`), so first in, first out comes
from the file name as now. On each check, a waiting run lists the live
tickets, reads their changes' priorities and `depends_on` from the
checkout, and sorts by the key above. Only the first may try for a slot.

Reading live means a reorder applies to runs already queued. Writing the
rank into the ticket name was rejected: a drag would then not affect
queued runs, which is when the owner most wants it.

### Board
- Cards gain `priority` (value or null) and `position`.
- Proposed, Approved and In progress sort by priority order. So do Queued
  and the step columns in the steps layout. Ready and Done keep their
  current order (by time).
- The mod allows drags within a sorted column only. On a drop it computes
  the target position from the card it lands next to:
  - above card B: B's position if the card came from below B, else one less
  - below the column's last card L: L's position if the card came from
    above L, else one more
- It then runs `tuckerbox priority <change> <n>` on its own machine and
  waits for the next read. If positions moved between the read and the
  drop, the card may land one place off, and the next read shows the truth.

### Where the requirements live
`add-herdr-board` isn't archived, so the board requirements stay in
`change-priority`. `change-runs` is archived, so `Parallel limit` gets a
MODIFIED delta: "in the order requested" becomes "in admission order".

### Autopilot
`add-autopilot` picks up approved changes in admission order. This change
defines the order. It adds nothing autopilot-specific.

## Risks / Trade-offs

- **Starvation by design.** A low-ranked change may never run while higher
  ones keep arriving. The board shows its position, so it is visible.
- **Order differs between machines** until priorities are committed and
  pulled. Each machine's queue is its own, so this only matters for the
  board, which uses its own machine's order.
- **A checkout behind the base branch** may lack a new change's folder.
  `priority` then refuses for that change and says to pull. The order puts
  it with the unranked changes.
- **Live reads cost.** Each queue check reads a few small files per queued
  run, once per poll. That is small next to `board --json`.
