# Proposal

## Why

Tuckerbox starts queued changes in the order they were requested, and the
board lists changes in no useful order. The owner can't say "this one
next". As Tuckerbox becomes a software factory (approved OpenSpec in,
merged features out), the order of the queue is the main thing the owner
still decides, apart from approval. They want to set it by dragging cards
on the board.

## What Changes

- **A rank per change:** `priority = <n>` in the change's
  `openspec/changes/<id>/tuckerbox.toml`. Lower runs first. A change
  without one comes after every ranked change. It is an ordinal rank, not a
  1 to 5 scale (see design.md).
- **`tuckerbox priority`** lists active changes in priority order. `tuckerbox
  priority <change> <n|up|down|top>` moves a change to position `n`, one
  place up or down, or to the top. Tuckerbox picks the stored numbers, with
  gaps, so a move usually rewrites one file. It works in the repository
  checkout, while runs are queued or running, and never commits.
- **Admission order:** `run --all`, the slot queue and `add-autopilot`
  use one order: dependencies first, then priority value, then first in,
  first out (request order, never age). A dependency moves up to the rank
  of the most urgent change waiting on it. The order is built one change
  at a time, so it is always a valid dependency order. Reordering a queued
  change takes effect for the next free slot.
- **Consistent admission:** a run chooses the first queued run and takes a
  slot in one step under the sidecar lock, and a `priority` move holds that
  lock for all its writes. So no run is admitted on a stale or half-written
  order. TLA+ model `formal/priority.tla`, with two broken variants TLC
  rejects.
- **Board:** Proposed, Approved, In progress (and in the steps layout,
  Queued and each step column) sort by priority, then age. The owner drags
  a card within its column to reorder it. The board runs
  `tuckerbox priority` to do it.
- **Approval is kept:** `tuckerbox.toml` is not part of the approval hash,
  so a priority change never makes an approval stale.
- **One writer at a time for `tuckerbox.toml`:** `priority`, `approve`,
  `unapprove` (and `set`, if `add-change-settings` lands) take a short
  repository lock around each rewrite, so two of them can't lose each
  other's edit.

## Capabilities

### New Capabilities

- `change-priority`: the rank, the `priority` command, the priority and
  admission orders, and how priority writes keep approval.

### Modified Capabilities

- `change-runs`: `Parallel limit` starts queued runs in admission order
  instead of request order.
- `board-data`: cards carry `priority` and `position`, and the sorted
  columns list cards in priority order.
- `herdr-board`: drag a card within its column to reorder it.

The board additions were first written in `change-priority` and moved to
`board-data` and `herdr-board` once `add-herdr-board` was archived. See
design.md.

## Impact

- Depends on `add-herdr-board` (sidecar, dependencies, board data, the mod).
- The slot queue's ordering in the run engine changes. The slot and change
  locks don't. No new state file, apart from a lock file in the shared
  `tuckerbox` folder of the repository's git dir.
- New TLA+ model `formal/priority.tla` for the order. `formal/runs.tla` is
  unchanged: it allows any order, so it can't check this one. See
  model.md.
- Existing changes have no `priority`, so the queue keeps today's order
  among them: first requested, first started.
