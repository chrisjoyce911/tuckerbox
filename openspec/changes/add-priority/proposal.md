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
  use one order: dependencies first, then priority, then first in, first
  out. A dependency moves up to the rank of the most urgent change waiting
  on it. Reordering a queued change takes effect for the next free slot.
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
  admission orders, how priority writes keep approval, and priority on the
  board.

### Modified Capabilities

- `change-runs`: `Parallel limit` starts queued runs in admission order
  instead of request order.

The board additions are in `change-priority`, because `add-herdr-board`
isn't archived yet. See design.md.

## Impact

- Depends on `add-herdr-board` (sidecar, dependencies, board data, the mod).
- The slot queue's ordering in the run engine changes. The slot and change
  locks don't. No new state file, apart from a lock file in the shared
  `tuckerbox` folder of the repository's git dir.
- No TLA+ change: `formal/runs.tla` already allows any queued run to take
  a free slot, so it covers every order. See model.md.
- Existing changes have no `priority` and keep today's order among
  themselves (oldest first).
