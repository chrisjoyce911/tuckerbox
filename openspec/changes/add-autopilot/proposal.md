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
- **Shipping stays the run's:** when the last group is complete, the run
  ships through `add-forge-shipping`, which archives the change in the
  merge request and, in `merge` mode, merges it. When a change counts as
  complete, `(ship)` tasks such as "Archive this change", and what happens
  to partial or incomplete runs are `add-task-scope`'s rules (from
  `fix-task-scope`). Autopilot adds none of its own.
- **Approval is the one gate:** autopilot starts only approved changes,
  through the normal run gate, and refuses to run when `require_approval`
  is off. It never approves, edits, resumes a blocked or stopped run, or
  re-runs a failed one.
- **It acts only on what it saw:** each start or resume carries the state
  and `since` it read. Under the change lock, the run engine goes ahead
  only if both still match and the approval rule holds: a start needs
  `approved`, a crash resume needs `approved` or `stale` (not `none`).
  Otherwise it does nothing. TLA+ model `formal/autopilot.tla`, with four
  broken variants TLC rejects.
- **Crashes:** a run whose process died is resumed once per crash streak:
  the count runs from the last step result or person action, read from
  history, not from rounds (every resume raises the round). A second crash
  without progress is left for a person.
- **Modes:** in `solo` mode autopilot starts runs on its machine. In
  `fleet` mode each pass runs `tuckerbox dispatch` (`add-operating-modes`).
  `team` mode is refused for now.
- **Stopping for a person:** when a run becomes `blocked` or `failed`,
  autopilot leaves a crashed run, or the next group is refused because the approval went stale, autopilot
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

None. When a change is complete, and what partial or incomplete runs do,
are `task-scope` rules (see design.md).

## Impact

- Depends on `add-task-scope` (`--tasks next`, completeness), `add-priority`
  (admission order), `add-forge-shipping` (shipping, archive, merge) and
  `add-operating-modes` (fleet `dispatch`). Without forge
  shipping it still works with `ship = "local"`: changes stop at `ready`.
- New command, new `[autopilot]` keys (`scope`, `interval`), a lock and a
  marker in the shared `tuckerbox` folder of the repository's git dir.
- The run engine gets conditional requests (start or resume only if the run
  is still in a given state).
- In fleet mode, placement is `add-operating-modes`' `tuckerbox dispatch`.
- This repository ships with `ship = "merge"`: `AGENTS.md` lets the
  coordinating agent merge after green CI and a cross-model review with no
  open findings, and approval is the owner's only routine gate. Merge mode
  merges when the forge says mergeable, so the cross-model review has to
  be a forge rule (a required approving review). Until it is, the
  repository uses `ship = "mr"` and the coordinating agent merges.
