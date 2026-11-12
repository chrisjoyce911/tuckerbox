# Design

## Context

See proposal.md for why. The rules being fixed are in `add-herdr-board` (active, built and merged as code): `herdr-board`'s `Install record`, `Toggle key`, `Adding the drawer`, `Uninstalling`, `Which repository`, `Docking the board` and `What the drawer shows`, and `board-data`'s `Card summary` and `Archived approvals`. Findings 1–6 come from the review of PR #25; 7–9 were seen live on the board.

This is the first change Tuckerbox builds through its own board: the owner approves it, `tuckerbox run` builds it with Claude and reviews it with Codex. So it changes no config, flag or JSON format, and every requirement maps to tests.

## Goals / Non-Goals

**Goals:**
- An install or uninstall never leaves a Herdr file changed without a record that can undo it.
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
| Failed record write rolls back | herdr-board: Install record ("a step that fails SHALL leave its files unchanged") |
| Failed drawer step after a clone | herdr-board: Adding the drawer, Install record |
| Re-install with another key | herdr-board: Toggle key, Toggle key never replaces a binding |
| The repository's main checkout | herdr-board: Which repository, Docking the board |
| Explicit machine pin | herdr-board: Which repository, Docking the board |
| Stale reads are dropped | herdr-board: What the drawer shows, Boards follow their own space |
| No approval badge on Done cards | board-data: Archived approvals (the data stays `none`; the mod hides it) |
| Titles of this repository's changes | board-data: Card summary |

No `depends_on = ["add-herdr-board"]`: a run waits until each dependency is `ready` on this machine or archived, and `add-herdr-board` is neither, so the dependency would hold this run forever. The code it fixes is already on the base branch.

### Install lock

`internal/herdrmod` takes an exclusive lock on `<herdr home>/.tuckerbox-install.lock` with the OS lock the change lock uses in `internal/runs/lock.go` (`flock` on Unix, `LockFileEx` on Windows), moved to a shared helper if needed, for the whole of install or uninstall: read the record, change files, write the record. Atomic renames make each file write whole, but don't serialise read, modify, write; the lock does. It waits with a 30-second limit, polling every 100 ms, because Herdr's own `layout.clone` call can take a moment. The lock file is never removed (removing it would race a waiter), and is not in the record.

### Rollback

Each run keeps a journal in memory of what it changed: for each file, its bytes before (or "absent"), and a clone it made with the layout shown before. On any failure after the first change, including writing the record, it replays the journal backwards: restore bytes, switch back to the earlier layout, remove the clone. The record is written last, so a failed record write leaves the old one. If the replay itself fails, install writes a record that covers what is still changed, so `uninstall` can finish the job, and says so. This is the "Clean-up fails" scenario.

### Re-install with another key

The record already keeps `keys.json` from before the first install and the key it added. A re-install with a different `--key` checks the new key for clashes (ignoring the board's own old binding), removes the old binding, adds the new one, and replaces the recorded key. The recorded "before" bytes stay those from the first install, so uninstall still restores the file byte for byte if the person changed nothing else.

### Main checkout

`data.js`' `look` runs `git rev-parse --show-toplevel` today, which in a linked worktree gives the worktree. Instead it runs `git worktree list --porcelain` and takes the first `worktree` entry (the main worktree), unless that entry is `bare`, then falls back to `--show-toplevel`. The probe cache key stays the folder and machine. Docked boards resolve their `folder` prop the same way, so a pane docked on a worktree before this fix reads the main checkout too.

Why the main worktree: it is where the person keeps the repository and where `tuckerbox board` sees every change. A change worktree is on the change's branch: its `openspec/changes/` and config can be behind or ahead of the base, and its board would list one machine's view of one branch.

### Explicit machine pin

`repoCandidates` uses `p.machine ?? …`, which treats `null` as absent. It becomes `"machine" in p ? p.machine : (p.on ?? space?.machine ?? null)`. Docking already writes `machine: null` for this computer.

### Stale reads

Both views keep a `want` key, the candidate folders and machine joined, updated on every space or focus change. A read records the key it started with. On completion:
- if its key isn't the current `want`, the result is dropped and, unless a read for `want` is already running, one starts at once
- otherwise it is applied as today

`loading` no longer blocks a refresh for a new key; it only stops a second read for the same key. Tested with the real `DrawerView` and board view and a delayed fake `exec`.

### Done cards

`board-data` keeps approval `none` on Done cards, as `Archived approvals` says. The mod's card and card detail skip the approval chip for column `done`. Nothing changes in the JSON.

### Titles

Two options were asked about: write `title` in each change's `tuckerbox.toml`, or derive a title from the proposal's first heading when it isn't the template's `Proposal`. We keep `title`:
- Every proposal in this repository starts with `# Proposal`, the schema template's heading. Deriving would give nothing here.
- `board-data`'s `Card summary` already says the heading is not used, for that reason. One source of titles is simpler to explain and test.
- `title` is a Tuckerbox key in Tuckerbox's file. A heading belongs to OpenSpec's file and its templates.

`add-herdr-board` has no `tuckerbox.toml`; this change adds one with `title = "Herdr board"` only. The sidecar is not part of the approval hash, so its approval is unaffected. A Go test (`internal/board`, `TestRepositoryChangesHaveTitles`) reads `openspec/changes/*/` (not `archive/`) from the repository root and fails naming each change without a non-empty `title`.

### Model

None (model.md). One process writes the install files, under the new lock: the same OS-lock pattern `formal/runs.tla` checks for the change lock. The stale-read fix is inside one mod process, one event loop.

## Risks / Trade-offs

- **[A hung `layout.clone` holds the lock]** → `herdr-app do` already has a timeout; the waiter's 30-second limit reports the wait rather than hanging.
- **[Rollback can't undo the layout switch if Herdr quit meanwhile]** → The clone is then recorded and the output says to run `uninstall` with Herdr running.
- **[`git worktree list` on a remote machine is slower]** → One more git call per probe; the probe cache (30 s for found, 10 s for not) is unchanged.
- **[Repositories where the main worktree is a bare clone]** → Fall back to the folder's own top level, which is today's behaviour.
- **[The title test makes forgetting a title fail CI]** → Intended. The fix is one line.

## Migration Plan

Nothing to migrate. Existing install records keep working: the format doesn't change. Docked boards on a worktree folder start reading the main checkout on the next refresh.
