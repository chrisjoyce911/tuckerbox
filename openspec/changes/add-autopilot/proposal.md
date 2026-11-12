# Proposal

## Why

The owner wants Tuckerbox to be a software factory: approved OpenSpec in,
merged features out, with as little human work as possible. Today a person
still starts every run, starts the next group of a large change, and
notices when something finishes. Approval should be the one human gate,
and a person should be needed again only when a run is blocked.

Muster and Kelpie keep work moving with a long-lived process that watches
the board. We want the same without a system daemon: the loop runs in a
terminal (or a Herdr pane) the owner can see and stop.

## What Changes

- **`tuckerbox autopilot`:** a foreground loop for one repository on one
  machine. On each pass it fetches the base branch, takes the active
  changes in admission order (`add-priority`: dependencies, then priority,
  then first in, first out), and starts approved changes while
  `max_parallel` has room. It runs until Ctrl-C. Runs it started keep
  going when it exits. `--once` does one pass. `--dry-run` only prints
  what it would start.
- **Group by group:** with `[autopilot] scope = "group"` (the default) it
  starts each change with `--tasks next` (`add-task-scope`). When a run is
  ready and tasks are left, it starts the next group. `scope = "change"`
  runs whole changes.
- **Partial runs don't ship:** in `mr` or `merge` mode, a run whose change
  still has unticked tasks ends `ready` at its last step and doesn't ship.
  This applies to every run, not only autopilot's. Once the last group is
  ticked, the run ships through `add-forge-shipping`, which archives the
  change in the merge request and, in `merge` mode, merges it.
- **Approval is the one gate:** autopilot starts only approved changes,
  through the normal run gate, and refuses to run when `require_approval`
  is off. It never approves, edits, resumes a blocked or stopped run, or
  re-runs a failed one.
- **It acts only on what it saw:** each start or resume is conditional. The
  run engine checks, under the change lock, that the run is still in the
  state autopilot saw and the change is still approved. Otherwise it does
  nothing. TLA+ model `formal/autopilot.tla`, with a broken variant TLC
  rejects.
- **Crashes:** a run whose process died is resumed once per step. A second
  crash in the same step is left for a person.
- **Stopping for a person:** when a run becomes `blocked` or `failed`, or
  the next group is refused because the approval went stale, autopilot
  prints it once and carries on with other changes. The board shows those
  cards as needing you, and the mod shows a Herdr toast when one appears.
- **Restarts:** autopilot keeps no state of its own. Everything comes from
  OpenSpec, `run.json` and history, so after a restart or reboot it picks
  up where things are. A lock and a small marker file say it is running.

## Capabilities

### New Capabilities

- `autopilot`: the loop: what it starts and in what order, group by group,
  what it never does, crashes, restarts, and how it asks for a person.
- `autopilot-board`: autopilot on the board: whether it is on, which cards
  need the person, and toasts. It adds to `board-data` and `herdr-board`
  once `add-herdr-board` is archived.

### Modified Capabilities

None. "Partial runs don't ship" adds to `forge-shipping`, which isn't
archived yet, so it is kept in `autopilot` (see design.md).

## Impact

- Depends on `add-task-scope` (`--tasks next`), `add-priority` (admission
  order) and `add-forge-shipping` (shipping, archive, merge). Without forge
  shipping it still works with `ship = "local"`: changes stop at `ready`.
- New command, new `[autopilot]` keys (`scope`, `interval`), a lock and a
  marker in the shared `tuckerbox` folder of the repository's git dir.
- The run engine gets conditional requests (start or resume only if the run
  is still in a given state).
- One machine per repository. Several machines sharing a repository's
  changes is `add-operating-modes`' job.
- This repository's rule stays: the owner merges. Here autopilot would run
  with `ship = "mr"` and stop at a green merge request.
