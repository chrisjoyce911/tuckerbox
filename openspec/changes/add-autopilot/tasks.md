# Tasks

## 1. The command and its lock (autopilot)

- [x] 1.1 `tuckerbox autopilot [--once] [--dry-run]`: foreground loop; refuse when `require_approval` is false
- [x] 1.2 `<common-dir>/tuckerbox/autopilot.lock` and `autopilot.json` (pid, machine, start time), counted only while the lock is held; release on Ctrl-C or SIGTERM; runs keep going
- [x] 1.3 `[autopilot] scope` (`group`, `change`) and `interval` (at least 10 s, default 60 s) in either config, repository winning; `tuckerbox check` validates them
- [x] 1.4 Test: Approval off
- [x] 1.5 Test: Second autopilot
- [x] 1.6 Test: Dry run
- [x] 1.7 Test: Interval too short
- [x] 1.8 Test: Restart mid-run

## 2. Passes (autopilot)

- [x] 2.1 Watch the repository's run states every 5 s; a full pass when a run ends or the interval passes: fetch the base (warn when offline), active changes in admission order (`add-priority`)
- [x] 2.2 Decide per change: approved (run gate), dependencies met (engine rule, read-only), no run or a ready run with unticked tasks; start until queued and running reach `max_parallel`
- [x] 2.3 Start with `--tasks next` in group mode, no scope in change mode
- [x] 2.4 Test: Within max_parallel
- [x] 2.5 Test: Unapproved change
- [x] 2.6 Test: Dependency not met
- [x] 2.7 Test: Next group
- [x] 2.8 Test: Whole changes

## 3. Conditional requests and crashes (autopilot)

- [x] 3.1 Run engine requests carry the expected state and `since`; under the change lock the engine goes ahead only if both match and the approval rule holds (start: `approved`; crash resume: not `none`), else changes nothing
- [x] 3.2 Never approve, edit, tick, resume blocked or person-stopped runs, or re-run failed ones; skip changes whose lock an edit holds
- [x] 3.3 Crash streak from history: autopilot's `resumed` events carry `by: autopilot`; count them since the last `step_finished` or person `started`/`resumed`/`rerun`/`scope_started`; resume only at 0; leave and report otherwise
- [x] 3.4 `formal/autopilot.tla` passes and `autopilot_broken_unchecked`, `_nosince`, `_ungated` and `_perround` are rejected by `scripts/tla-check.sh`
- [x] 3.5 Test: Person stopped a run
- [x] 3.6 Test: Blocked run
- [x] 3.7 Test: Unapproved after the pass looked
- [x] 3.8 Test: Stopped after the pass looked
- [x] 3.9 Test: Crashed again after a person resumed
- [x] 3.10 Test: Unapproved while crashed
- [x] 3.11 Test: Stale approval from the run's own step
- [x] 3.12 Test: Machine restarted
- [x] 3.13 Test: Crashes twice across rounds (rounds 1, 2 and 3; with an autopilot restart between the crashes)
- [x] 3.14 Test: Progress renews the budget
- [x] 3.15 Test: A person renews the budget

## 4. Shipping and modes (autopilot)

- [x] 4.1 Rely on task-scope's completeness rules; autopilot never archives, pushes or merges; a change archived on the base is finished
- [ ] 4.2 Modes: `solo` starts runs here; `fleet` runs `tuckerbox dispatch` each pass; `team` refuses
- [x] 4.3 Test: First group in merge mode
- [ ] 4.4 Test: Last group in merge mode (with a real `(ship)` archive task in the change)
- [x] 4.5 Test: Whole change left incomplete
- [x] 4.6 Incomplete ready runs: continue with `--tasks next` when the run has a scope or in group mode; in change mode report once, request no start, mark the card `needs_you`
- [x] 4.7 Test: Whole change left ready in local mode (several passes, no start requested)
- [x] 4.8 Test: Group mode picks up the rest
- [x] 4.9 Test: Whole change left ready (board)
- [ ] 4.10 Test: Fleet mode
- [ ] 4.11 Test: Team mode

## 5. Asking for a person (autopilot, autopilot-board)

- [x] 5.1 Print one line per run that newly needs a person (blocked, failed, crash budget spent, stale approval before a next group); keep the reported set in memory
- [x] 5.2 Board document: `autopilot` (running, pid, start time); `needs_you` with a reason on cards; header count uses it
- [x] 5.3 Mod: Herdr toast for cards that newly need you between two reads (board and drawer), none at the first read; autopilot on or off in the header
- [x] 5.4 Test: Blocked review
- [x] 5.5 Test: Stale before the next group
- [x] 5.6 Test: Autopilot running
- [x] 5.7 Test: Leftover marker
- [x] 5.8 Test: Next group waits for approval
- [x] 5.9 Test: Run blocks
- [x] 5.10 Test: Board opened later
- [x] 5.11 Test: Autopilot off

## 6. Wrap-up

- [x] 6.1 README: `tuckerbox autopilot`, that it is not a service, approval as the one gate, and running it in a Herdr pane
- [x] 6.2 `go vet ./...`, `go test -race ./...` and `scripts/tla-check.sh` pass
- [ ] 6.3 (ship) Archive after `add-task-scope`, `add-priority`, `add-forge-shipping` and `add-operating-modes`, once accepted; move the board requirements into `board-data` and `herdr-board`
