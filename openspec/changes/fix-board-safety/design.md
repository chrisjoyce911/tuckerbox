# Design

## Context

See proposal.md for why. The rules being fixed are in `add-herdr-board` (active, built and merged as code): `herdr-board`'s `Install record`, `Toggle key`, `Adding the drawer`, `Uninstalling`, `Which repository`, `Docking the board` and `What the drawer shows`, and `board-data`'s `Card summary` and `Archived approvals`. Findings 1–6 come from the review of PR #25; 7–10 were seen live on the board. The review of PR #29 then asked for a durable journal, a defined way to undo a clone left behind, shift-wheel scrolling that Herdr's real API supports (now left to Herdr and the operating system), and a model of the stale-read race; those are folded in.

This is the first change Tuckerbox builds through its own board: the owner approves it, `tuckerbox run` builds it with Claude and reviews it with Codex. So it changes no config, flag or JSON format, and every requirement maps to tests.

## Goals / Non-Goals

**Goals:**
- An install or uninstall never leaves a Herdr file changed without a record or journal on disk that can undo it, except when even the journal can't be written, and then it changes nothing.
- The board always reads the repository the person means, on the machine they mean, and never shows a result for a space they left.

**Non-Goals:**
- New install options, or a new record format. The record gains no fields; a re-install's new key replaces the old one in it.
- Making `tuckerbox board` itself resolve linked worktrees. The CLI keeps reading where it runs; only the mod's choice of folder changes.
- Deriving titles from proposals (see "Titles").

## Decisions

### Why ADDED requirements in new capabilities

The natural form is MODIFIED deltas against `herdr-board` and `board-data`. But those specs exist only inside `add-herdr-board`, which isn't archived. `openspec validate --strict` accepts such a delta, then warns: "Archive would refuse this delta: target spec does not exist; only ADDED requirements are allowed for new specs." So, as `add-forge-shipping` does with `ship-board`, the fixes are ADDED requirements in `board-install` and `board-view`. Each says which `herdr-board` or `board-data` rule it narrows (below). Folding them in is a tidy-up once both changes are archived.

| New requirement | Narrows |
|---|---|
| One install at a time per Herdr home | herdr-board: Install record |
| Install journal, Failed record write rolls back, Recovering from a left journal | herdr-board: Install record ("a step that fails SHALL leave its files unchanged") |
| Failed drawer step after a clone, Uninstalling a clone it can't read | herdr-board: Adding the drawer, Install record, Uninstalling, Uninstalling a cloned layout |
| Re-install with another key | herdr-board: Toggle key, Toggle key never replaces a binding |
| The repository's main checkout | herdr-board: Which repository, Docking the board |
| Explicit machine pin | herdr-board: Which repository, Docking the board |
| Stale reads are dropped | herdr-board: What the drawer shows, Boards follow their own space |
| No approval badge on Done cards | board-data: Archived approvals (the data stays `none`; the mod hides it) |
| Columns scroll sideways | herdr-board: Docking the board, Card detail view |
| Titles of this repository's changes | board-data: Card summary |

No `depends_on = ["add-herdr-board"]`: a run waits until each dependency is `ready` on this machine or archived, and `add-herdr-board` is neither, so the dependency would hold this run forever. The code it fixes is already on the base branch.

### Install lock

`internal/herdrmod` takes an exclusive lock on `<herdr home>/.tuckerbox-install.lock` with the OS lock the change lock uses in `internal/runs/lock.go` (`flock` on Unix, `LockFileEx` on Windows), moved to a shared helper if needed, for the whole of install or uninstall: read the record, change files, write the record. Atomic renames make each file write whole, but don't serialise read, modify, write; the lock does. It waits with a 30-second limit, polling every 100 ms, because Herdr's own `layout.clone` call can take a moment. The lock file is never removed (removing it would race a waiter), and is not in the record.

### Rollback and the journal

A journal in memory dies with the process, and the record can't hold undo information when writing the record is what failed. So the journal is a file, written before anything changes.

`.tuckerbox-install.journal.json` in the Herdr home, next to the record:

```json
{"version": 1, "op": "install",
 "entries": [
   {"file": "keys.json", "before": "<bytes as they were, or null for absent>", "after": "sha256:…"},
   {"clone": "main-tuckerbox", "from": "main", "shown_before": "main", "after": "sha256:… or null"}
 ]}
```

- **Written first, durably:** written to a temporary file, synced, and renamed into place, before the first change. If that fails, nothing has changed and install exits non-zero naming the journal file.
- **Write-ahead:** each entry's `after` is the hash install is about to write, so the journal is complete before the file changes. A clone's hash isn't known until Herdr makes it, so its entry is written with `after: null` before calling `layout.clone` and updated with the hash straight after.
- **Success:** the record is written last; then the journal is removed.
- **Failure:** the run undoes its entries backwards (restore `before`, or remove a file that was absent; show `shown_before`, remove the clone). If every undo worked, the journal is removed. If one failed, the journal stays, install exits non-zero, and says the next `tuckerbox herdr install` or `uninstall` finishes the undo.
- **Recovery:** every install and uninstall, once it holds the lock, first looks for a journal and undoes it the same way, comparing each file's current hash with the entry's `after`:
  - a match is undone, and the entry is dropped from the journal (rewritten durably) once undone
  - a file the person changed since is reported, left as it is, and dropped: that is a deliberate keep, not a failure
  - a clone with `after: null` (the process died between cloning and updating the journal) is reported and left, since nothing proves it is unchanged
  - if an undo fails (a write, a layout switch or a removal), recovery stops: the journal keeps every entry not yet undone, the command exits non-zero naming what failed, and **no new change starts**. Install and uninstall both refuse to do anything else while a journal can't be recovered, so a new transaction can never overwrite the only copy of earlier bytes.
  - once every entry is undone or dropped, the journal is removed and the command carries on

Undo is idempotent: a file already back to `before` is skipped. So a crash during recovery is recovered by the next run.

This is a bounded contract, not a promise that nothing can go wrong: if the disk can't hold even the journal, install changes nothing; once the journal exists, every change it makes has undo information on disk until it is undone or recorded.

### A clone left behind

When the drawer step fails after cloning and removing the clone fails too, the record (when it can be written) holds the clone in the existing drawer entry format, so uninstall needs no new field:
- `layout`: the clone, `cloned_from`: its source (which was the shown layout, because install clones the shown one)
- `before`: empty, since a clone is removed, never restored
- `after`: the hash of the clone as found, however malformed

`uninstallDrawer` today parses the layout before checking its hash, so a malformed clone stops it. It changes to compare the hash first: a clone still as recorded is removed without parsing, after switching back to `cloned_from` if it is shown. Only a layout that changed since is parsed, to take out the drawer block; if that layout no longer parses, it is left and reported, and uninstall goes on with the key and the mod folders rather than stopping.

### Re-install with another key

The record already keeps `keys.json` from before the first install and the key it added. A re-install with a different `--key` checks the new key for clashes (ignoring the board's own old binding), removes the old binding, adds the new one, and replaces the recorded key. The recorded "before" bytes stay those from the first install, so uninstall still restores the file byte for byte if the person changed nothing else. If the file changed since the last install, the recorded "before" becomes the file as it was just before the re-install minus the board's old binding, so uninstall keeps the person's edits.

### Main checkout

`data.js`' `look` runs `git rev-parse --show-toplevel` today, which in a linked worktree gives the worktree. Instead it runs `git worktree list --porcelain` and takes the first `worktree` entry (the main worktree), unless that entry is `bare`, then falls back to `--show-toplevel`. The probe cache key stays the folder and machine. Docked boards resolve their `folder` prop the same way, so a pane docked on a worktree before this fix reads the main checkout too.

Why the main worktree: it is where the person keeps the repository and where `tuckerbox board` sees every change. A change worktree is on the change's branch: its `openspec/changes/` and config can be behind or ahead of the base, and its board would list one machine's view of one branch.

### Explicit machine pin

`repoCandidates` uses `p.machine ?? …`, which treats `null` as absent. It becomes `"machine" in p ? p.machine : (p.on ?? space?.machine ?? null)`. Docking already writes `machine: null` for this computer.

### Stale reads

Both views keep `want`, the candidate key (folders and machine joined), updated on every space or focus change. When `want` changes, the view drops what it shows (the drawer draws nothing, the board shows its loading state) and starts a read for the new key at once. `loading` becomes a set of keys with a read running, so a read for a new key isn't blocked by one for an old key, and a second read for the same key doesn't start. A read records the key it started with; on completion it is applied only if that key is still `want`, and dropped otherwise.

This is modelled in `formal/boardview.tla` (model.md): switches including A → B → A, overlapping reads, completions in any order. `ShownIsCurrent` says the view never shows a result for a key that isn't current; `Settles` says it comes to show the current key's result once switches stop. The broken variant `boardview_broken_anykey` applies whatever read finishes, as the old code did, and TLC finds the 4-state race: read A starts, the space switches to B, A's read finishes and A is shown in B.

Tested with the real `DrawerView` and board view and a delayed fake `exec` whose completions the test releases in a chosen order.

### Done cards

`board-data` keeps approval `none` on Done cards, as `Archived approvals` says. The mod's card and card detail skip the approval chip for column `done`. Nothing changes in the JSON.

### Columns scroll sideways

The board uses Herdr's own horizontal scrolling and adds no wheel handling of its own:
- The column row is `overflow_x_scrollbar()` (from Herdr's `gpui-kit.d.ts`): a scroll area with a scrollbar that can be dragged, and that takes trackpad swipes.
- Shift with the mouse wheel works where the operating system turns it into a sideways movement (macOS does), because Herdr's scroll area then receives a sideways delta. Where it arrives as a vertical movement, it goes to whatever scrolls vertically under the pointer, usually a column's list. The requirement promises nothing for a plain vertical wheel.

An earlier draft stepped columns on shift with the wheel through `ScrollHandle.scroll_to_item`. It was dropped: Herdr gives a script no way to read the scroll offset, so after a scrollbar drag the script's idea of the first visible column is wrong, and "reveal the next column" can scroll the wrong way. Behaviour Herdr already provides is correct by construction; a script's guess at it isn't.

The board root is already `size_full()` and the row already has `overflow_x_scrollbar()`, so why Ready and Done were unreachable in a narrow docked pane is not yet known. The first task reproduces it in a 500 px docked pane and records the cause in the implementing pull request before changing layout; nothing here assumes a cause.

The tests use a strict stub of the Herdr API that throws on any call the installed `gpui-kit.d.ts` doesn't declare: a committed list of the members used, with the Herdr version it was taken from. A check script compares that list with the installed `gpui-kit.d.ts` when one is present (on a developer's machine); CI, which has no Herdr, uses the committed list.

A stacked layout of collapsible sections for narrow panes is not part of this change. The root's `on_size_change` gives the width, so it is possible, but it is a second layout to design and test, and this change is meant to be small. A later change can add it.
### Titles

Two options were asked about: write `title` in each change's `tuckerbox.toml`, or derive a title from the proposal's first heading when it isn't the template's `Proposal`. We keep `title`:
- Every proposal in this repository starts with `# Proposal`, the schema template's heading. Deriving would give nothing here.
- `board-data`'s `Card summary` already says the heading is not used, for that reason. One source of titles is simpler to explain and test.
- `title` is a Tuckerbox key in Tuckerbox's file. A heading belongs to OpenSpec's file and its templates.

`add-herdr-board` has no `tuckerbox.toml`; this change adds one with `title = "Herdr board"` only. The sidecar is not part of the approval hash, so its approval is unaffected. A Go test (`internal/board`, `TestRepositoryChangesHaveTitles`) reads `openspec/changes/*/` (not `archive/`) from the repository root and fails naming each change without a non-empty `title`.

### Model

`formal/boardview.tla` models the stale-read race (model.md). The install lock needs no new model: it is one OS lock around one process's work, the pattern `formal/runs.tla` checks for the change lock; the journal is a single writer under that lock.

## Risks / Trade-offs

- **[A hung `layout.clone` holds the lock]** → `herdr-app do` already has a timeout; the waiter's 30-second limit reports the wait rather than hanging.
- **[Rollback can't undo the layout switch if Herdr quit meanwhile]** → The journal (and the record, if it can be written) keeps the clone, and the output says to run `uninstall` with Herdr running.
- **[A journal left behind]** → It is undone by the next install or uninstall; entries changed since are reported, never overwritten.
- **[A recovery that keeps failing blocks install and uninstall]** → Intended: the journal holds the only copy of earlier bytes. The message names the file and the error (a permission, a full disk, Herdr not running), which the person fixes before trying again.
- **[`git worktree list` on a remote machine is slower]** → One more git call per probe; the probe cache (30 s for found, 10 s for not) is unchanged.
- **[Repositories where the main worktree is a bare clone]** → Fall back to the folder's own top level, which is today's behaviour.
- **[The title test makes forgetting a title fail CI]** → Intended. The fix is one line.

## Migration Plan

Nothing to migrate. Existing install records keep working: the format doesn't change. Docked boards on a worktree folder start reading the main checkout on the next refresh.
