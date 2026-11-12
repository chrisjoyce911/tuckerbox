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

### One lock for sidecar writes and admission
`approve` on a change without a worktree writes the checkout's sidecar with
no lock, because no run can use the checkout. With `priority` (and
`add-change-settings`' `set`), two commands can now rewrite the same file
at once, and one would lose the other's edit. Every sidecar writer takes
`<common-dir>/tuckerbox/sidecar.lock`, an OS file lock, around read, change
and write, and writes each file by a temporary file and a rename. A
`priority` move holds the lock until every file it changes is written, so
a renumber is one step for anyone else who takes the lock.

Admission takes the same lock (see "The queue reads priority live"). Lock
order is always change lock, then sidecar lock, then a slot lock taken
with a non-blocking try. The sidecar lock is held for milliseconds and
never while waiting, so the order can't deadlock.

### Two orders: display and admission
The **priority order** (priority, then age, then id) is total, so every
card has a position. It orders `tuckerbox priority`, the board and the
positions drags use.

The **admission order** decides who runs next. It must not use age:
among changes of equal rank, the one requested first must start first, as
`change-runs` requires today. So admission compares priority *values*,
not positions:

- **Rank:** the lowest `priority` value of the change and every active
  change that depends on it, directly or not. No value counts as after
  every value. Inheriting the rank is what "dependencies first" means in
  practice: without it, a top change could wait behind everything ranked
  above its low-priority dependency.
- **Procedure:** a sort with "a dependency before its dependent" as a
  tie-breaker is not a total order (A before C by request, C before B by
  request, B before A as A's dependency). So the order is built one change
  at a time instead (Kahn's algorithm with a priority queue):

  ```
  eligible = changes whose dependencies in the set are placed (or none)
  while eligible is not empty:
    take the one with the lowest (rank, request, id)
    place it; add changes whose dependencies are now all placed
  ```

  Every pick is deterministic, and the result is a valid dependency order
  because a change only becomes eligible after its dependencies.
- **Request:** in the queue, the ticket's time. For `run --all`, the
  changes are all requested at once, so their age (`created`, then id) is
  their request order, and `--all` requests them in admission order, so
  ticket times agree.

In the queue, the dependency step rarely matters: a run holds a ticket
only once its dependencies are met, so a dependency and its dependent are
not queued together. Rank inheritance still matters, because a queued
dependency inherits the rank of a dependent waiting outside the queue.

### The queue reads priority live, under the lock
Tickets keep their names (`<time>-<change>`), so the request order comes
from the file name as now. Writing the rank into the ticket name was
rejected: a drag would then not affect queued runs, which is when the
owner most wants it.

Choosing the first run and taking a slot must be one step. Otherwise:
- a run decides it is first, a move (or a better-ranked new ticket) lands,
  a slot frees, and the run takes it on its stale decision
- a run reads the priorities halfway through a renumber and picks the
  wrong first run

So a waiting run, on each check, takes the sidecar lock, lists the live
tickets, reads their changes' `priority` and `depends_on` from the
checkout, builds the admission order, and, if it is first, tries for a
slot. Then it lets the lock go. It never waits while holding the lock.
`formal/priority.tla` checks this. Its broken variants (deciding outside
the lock, and taking a slot without the lock) are rejected.

The consistency boundary: a run is admitted only if it is first by the
ranks of the last finished `priority` move. Edits outside Tuckerbox (an
editor, a `git pull` or checkout) take no lock. A `git pull` that changes
several files can be read half applied. Such edits count from the next
admission after they land, and the spec says so.

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
- **Live reads cost.** Each queue check takes the sidecar lock and reads a
  few small files per queued run, once per poll. That is small next to
  `board --json`. The lock serialises queue checks, which are milliseconds
  each.
- **Edits outside Tuckerbox aren't atomic.** A `git pull` that changes
  several priorities can be seen half applied by one admission. That is
  allowed: it counts from the next admission.
