# Tasks

## 1. Choosing a scope (task-scope)

- [x] 1.1 Parse `--tasks` (`N`, `N.M`, `N.M-N.M`, `next`) and resolve it against the tasks.md the run will use (worktree, else the gate's commit); resolve `next` to a group number
- [x] 1.2 Refuse unknown numbers, reversed ranges, all-ticked scopes and `--tasks` with `--all` or several changes, before making a worktree
- [x] 1.3 Test: Run one group
- [x] 1.4 Test: Range across groups
- [x] 1.5 Test: Next group
- [x] 1.6 Test: Unknown task
- [x] 1.7 Test: Scope already done
- [x] 1.8 Test: Scope with run all

## 2. Scoped runs in the engine (task-scope)

- [x] 2.1 Add `scope` (as typed, task numbers) to `run.json`; `resume` and `rerun` keep it
- [x] 2.2 `run` with a different scope on a ready, stopped, blocked or failed run starts again from the first step: round up, rejections cleared, same worktree; same scope behaves as today
- [x] 2.3 After the last step's `done`, check the scope's ticks in the worktree; `blocked` with `scope tasks not ticked: …` or a missing number
- [x] 2.4 Write `complete` into `run.json` in the same write that sets `ready` (under the change lock); clear it in the write that starts another scope; no scope counts as complete
- [x] 2.5 Dependency check (local mode): met when `ready` and `complete` from one read of the dependency's `run.json`, never its tasks.md; queued reason shows the scope
- [x] 2.6 Ship decision after the last step: in `mr`/`merge`, not complete means `ready` with nothing archived, pushed, opened or merged; unit-test the decision for every ship mode and `complete` value
- [x] 2.7 Test: Second scope while running
- [x] 2.8 Test: Resume keeps the scope
- [x] 2.9 Test: Next group after a ready one (round keeps counting, a late result from the old round is ignored)
- [x] 2.10 Test: Rest of the change
- [x] 2.11 Test: Ticks keep the next scope approved
- [x] 2.12 Test: Design edited during a scope
- [x] 2.13 Test: Scope ticked
- [x] 2.14 Test: Task left unticked
- [x] 2.15 Test: Dependency ready for one group
- [x] 2.16 Test: Last group ready (`ready` and `complete` in one `run.json` write)
- [x] 2.17 Test: Earlier group ready
- [x] 2.18 Test: Next group ticked while the dependent checks (interleaving test: start scope 2 and tick its tasks between the dependent's reads, with hooks in the engine; run with `-race`)
- [x] 2.19 Test: Dependency complete
- [x] 2.20 Test: Ticks by hand don't release dependents
- [x] 2.21 Test: First group in merge mode (unit test of the ship decision here; end-to-end in `add-forge-shipping`)
- [x] 2.22 Test: Last group in mr mode (unit test of the ship decision here; end-to-end in `add-forge-shipping`)
- [x] 2.23 Test: Dependents in merge mode (archive on the base required; end-to-end in `add-forge-shipping`)
- [ ] 2.24 Ship tasks: `(ship)` prefix; left out of scopes, `next` and `complete`; refuse a scope of only ship tasks; say in the brief not to tick them
- [x] 2.25 In `mr`/`merge`, a run without a scope that isn't complete after its last step is `blocked` with the unticked tasks
- [x] 2.26 Test: Final group with an archive task
- [x] 2.27 Test: Scope of a ship task
- [x] 2.28 Test: Whole-change run leaves a task (unit test of the decision here; end-to-end in `add-forge-shipping`)
- [x] 2.29 `formal/scope.tla` passes and `formal/scope_broken_split.tla` is rejected by `scripts/tla-check.sh`

## 3. Brief (agent-launch)

- [ ] 3.1 Add the Scope section to the brief: each task's number and text, and the in-scope rules
- [ ] 3.2 Test: Build after rejection still lists the findings with a scope set
- [ ] 3.3 Test: Brief for a scope

## 4. Status, history and board data (task-scope)

- [ ] 4.1 `status` shows the scope; `status --json` adds `scope` within the current version
- [ ] 4.2 History: `scope` on `started`, `resumed`, `rerun`, `ready` and `blocked`; new `scope_started` event; document both in `internal/history`
- [ ] 4.3 Board: `scope` on cards and rounds; `groups` (number, title, ticked, total) in card detail
- [ ] 4.4 Test: Next scope in history
- [ ] 4.5 Test: Status of a scoped run
- [ ] 4.6 Test: Groups in card detail

## 5. Board mod (task-scope)

- [ ] 5.1 Run group… menu in card detail, where Start is offered and on Ready cards with unticked tasks, listing groups with unticked tasks; runs `tuckerbox run <change> --tasks <n>`; shows command errors
- [ ] 5.2 Show the scope on cards
- [ ] 5.3 Test: Run the next group from a Ready card
- [ ] 5.4 Test: Nothing left to run
- [ ] 5.5 Test: Run group refused

## 6. Wrap-up

- [ ] 6.1 README: `--tasks`, `next`, and that approval covers the whole change
- [ ] 6.2 `go vet ./...`, `go test -race ./...` and `scripts/tla-check.sh` pass
- [ ] 6.3 (ship) Archive after `add-herdr-board`, once the implementation is accepted; fold the board, history and mod requirements into `board-data`, `run-history` and `herdr-board`
