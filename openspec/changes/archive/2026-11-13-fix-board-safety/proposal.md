# Proposal

## Why

A review of the board's Herdr install (PR #25) found four ways `tuckerbox
herdr install` can leave a person's Herdr files changed with no record to
undo them, and two ways the board mod can show the wrong repository. The
owner then saw three more board bugs live: a board read a change's worktree
instead of the repository, Done cards show a "not approved" badge, and the
board's own change is titled `add herdr board`. Later, in a narrow docked
pane, the columns overflowed and Ready and Done couldn't be reached.

This is the first change Tuckerbox builds through its own board, so it is
small and every rule has a test.

## What Changes

- **Install and uninstall take a lock per Herdr home**, held for the whole
  read, change and record, so two at once can't lose each other's record.
- **A journal file is written before anything changes,** so a failed
  record write rolls back, and when rollback fails too, the next install or
  uninstall finishes the undo. A recovery that fails keeps the journal and
  blocks any new change until it succeeds.
- **A failed drawer step after a layout clone** removes the clone and shows
  the layout that was shown before. A clone that can't be removed is
  recorded, and uninstall removes it even if it is malformed.
- **`--key` on a re-install is honoured:** the new key is checked for
  clashes and replaces the old binding; uninstall still restores the
  original `keys.json`.
- **Stale reads are dropped** in the board window and the drawer: a read
  that finishes after the space or its folders changed is discarded, and a
  read for the current space starts at once. TLA+ model
  `formal/boardview.tla` with a broken variant TLC rejects.
- **`machine: null` pins this computer.** An absent `machine` prop still
  means the space's machine.
- **The board finds the main checkout:** a pane in a change's worktree
  resolves to the repository's main worktree, never the change worktree.
- **Done cards show no approval badge.**
- **Columns scroll sideways** in the board window and a docked pane when
  they don't fit, at a fixed width each, with Herdr's own scrolling:
  scrollbar drag and trackpad, and shift with the wheel where the operating
  system turns it sideways. In a narrow docked pane Ready and Done were
  unreachable.
- **Titles come from `title` in each change's `tuckerbox.toml`,** as now.
  The proposal's heading is not used. Every active change in this
  repository gets a title, `add-herdr-board` gets `title = "Herdr board"`,
  and a test keeps it so.

## Capabilities

### New Capabilities

- `board-install`: how `tuckerbox herdr install` and `uninstall` stay safe
  when run twice at once, when a step fails, and on re-install.
- `board-view`: which repository and machine the board mod reads, how it
  handles reads that finish late, Done cards' badges, and change titles.

### Modified Capabilities

None. The rules these fix are in `herdr-board` and `board-data`, which
`add-herdr-board` defines and hasn't archived. OpenSpec refuses MODIFIED
deltas against a spec that doesn't exist yet, so these are ADDED
requirements in their own capabilities (see design.md).

## Impact

- Code: `internal/herdrmod` (install, uninstall, record, journal),
  `herdr/mods/tuckerbox-board/` (`data.js`, `model.js`, `drawer.js`,
  `board.js`, `card.js`), a repository test for change titles.
- Files: `openspec/changes/add-herdr-board/tuckerbox.toml` (new, title
  only; the sidecar is not in the approval hash).
- No config, CLI flag or JSON format changes. A new file in the Herdr home,
  `.tuckerbox-install.journal.json`, exists only while an install or
  uninstall is unfinished.
- Does not declare `depends_on = ["add-herdr-board"]`: a run waits until its
  dependencies are ready or archived, and `add-herdr-board` is neither.
