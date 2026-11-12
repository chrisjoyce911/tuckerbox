# Proposal

## Why

The owner asked: "when something's approved on the board, should that be
instantly committed?" Today `tuckerbox approve` only writes the `[approval]`
table into the change's `tuckerbox.toml` in the checkout. The run gate reads
the base branch, so an approval counts only after someone commits it, opens
a pull request and merges it. Meanwhile the card can fall back to Proposed
whenever the checkout changes. Approving is meant to be one click.

## What Changes

- **Approvals get their own ref:** `refs/tuckerbox/approvals` on the
  repository's remote, not protected and not a branch. Each approve and
  unapprove is one commit there, holding `approvals/<change>.toml`.
- **Published at once:** the commit is pushed straight away with
  `--force-with-lease` on the version just fetched. If someone else pushed
  first, Tuckerbox fetches and tries again, so concurrent approvals are
  never lost. TLA+ model `formal/approvals.tla`, with a broken variant TLC
  rejects.
- **The run gate and the board read approvals from that ref** (fetched).
  An entry there wins. Without one, an existing `[approval]` in the
  change's `tuckerbox.toml` on the base branch still counts.
- **The hash rules don't change:** an approval still holds the hash of the
  change's text (and settings), so editing the change still makes it
  stale.
- **Offline:** the commit is kept locally and counts on this machine. It is
  pushed with the next approve, unapprove or board fetch. The card shows
  `not published` until then.
- **Solo and fleet only.** In team mode, approvals keep coming only from
  the protected base branch (`add-operating-modes`), and `approve` works as
  today.

## Capabilities

### New Capabilities

- `approval-publish`: where approvals are stored and published, how the run
  gate and the board read them, offline approvals, and team mode's
  exception. It narrows `change-approval` (in `add-herdr-board`, not
  archived; see design.md).

### Modified Capabilities

None.

## Impact

- `internal/approval`: read and write the approvals ref; `approve` and
  `unapprove` stop writing `tuckerbox.toml` outside team mode.
- Run gate and `board --json`: read the ref; card field `published`.
- New ref on the remote. Pushing it needs only git's own push rights. CI
  doesn't run on it.
- Existing approvals in `tuckerbox.toml` keep working.
