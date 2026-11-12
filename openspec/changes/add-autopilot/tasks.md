# Tasks

## 1. The command and its lock (autopilot)

- [ ] 1.1 `tuckerbox autopilot [--once] [--dry-run]`: foreground loop; refuse when `require_approval` is false
- [ ] 1.2 `<common-dir>/tuckerbox/autopilot.lock` and `autopilot.json` (pid, machine, start time), counted only while the lock is held; release on Ctrl-C or SIGTERM; runs keep going
- [ ] 1.3 `[autopilot] scope` (`group`, `change`) and `interval` (at least 10 s, default 60 s) in either config, repository winning; `tuckerbox check` validates them
- [ ] 1.4 Test: Approval off
- [ ] 1.5 Test: Second autopilot
- [ ] 1.6 Test: Dry run
- [ ] 1.7 Test: Interval too short
- [ ] 1.8 Test: Restart mid-run

## 2. Passes (autopilot)

- [ ] 2.1 Watch the repository's run states every 5 s; a full pass when a run ends or the interval passes: fetch the base (warn when offline), active changes in admission order (`add-priority`)
- [ ] 2.2 Decide per change: approved (run gate), dependencies met (engine rule, read-only), no run or a ready run with unticked tasks; start until queued and running reach `max_parallel`
- [ ] 2.3 Start with `--tasks next` in group mode, no scope in change mode
- [ ] 2.4 Test: Within max_parallel
- [ ] 2.5 Test: Unapproved change
- [ ] 2.6 Test: Dependency not met
- [ ] 2.7 Test: Next group
- [ ] 2.8 Test: Whole changes

## 3. Conditional requests and crashes (autopilot)

- [ ] 3.1 Run engine requests carry the expected state and `since`; under the change lock the engine goes ahead only if both match and the gate passes, else changes nothing
- [ ] 3.2 Never approve, edit, tick, resume blocked or person-stopped runs, or re-run failed ones; skip changes whose lock an edit holds
- [ ] 3.3 Resume a crashed run once per step and round; `resumed` events carry `by: autopilot`; leave it after a second crash in the same step and round
- [ ] 3.4 `formal/autopilot.tla` passes and `formal/autopilot_broken_unchecked.tla` is rejected by `scripts/tla-check.sh`
- [ ] 3.5 Test: Person stopped a run
- [ ] 3.6 Test: Blocked run
- [ ] 3.7 Test: Unapproved after the pass looked
- [ ] 3.8 Test: Stopped after the pass looked
- [ ] 3.9 Test: Machine restarted
- [ ] 3.10 Test: Crashes twice

## 4. Shipping (autopilot, with add-forge-shipping)

- [ ] 4.1 In `mr` and `merge` modes, a run whose change has unticked tasks becomes `ready` after its last step without archiving or pushing
- [ ] 4.2 Test: First group in merge mode
- [ ] 4.3 Test: Last group in merge mode

## 5. Asking for a person (autopilot, autopilot-board)

- [ ] 5.1 Print one line per run that newly needs a person (blocked, failed, crash left, stale approval before a next group); keep the reported set in memory
- [ ] 5.2 Board document: `autopilot` (running, pid, start time); `needs_you` with a reason on cards; header count uses it
- [ ] 5.3 Mod: Herdr toast for cards that newly need you between two reads (board and drawer), none at the first read; autopilot on or off in the header
- [ ] 5.4 Test: Blocked review
- [ ] 5.5 Test: Stale before the next group
- [ ] 5.6 Test: Autopilot running
- [ ] 5.7 Test: Leftover marker
- [ ] 5.8 Test: Next group waits for approval
- [ ] 5.9 Test: Run blocks
- [ ] 5.10 Test: Board opened later
- [ ] 5.11 Test: Autopilot off

## 6. Wrap-up

- [ ] 6.1 README: `tuckerbox autopilot`, that it is not a service, approval as the one gate, and running it in a Herdr pane
- [ ] 6.2 `go vet ./...`, `go test -race ./...` and `scripts/tla-check.sh` pass
- [ ] 6.3 Archive after `add-task-scope`, `add-priority` and `add-forge-shipping`, once accepted; move "Partial runs don't ship" into `forge-shipping` and the board requirements into `board-data` and `herdr-board`
