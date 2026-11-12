# Tasks

## 1. Choosing a scope (task-scope)

- [ ] 1.1 Parse `--tasks` (`N`, `N.M`, `N.M-N.M`, `next`) and resolve it against the tasks.md the run will use (worktree, else the gate's commit); resolve `next` to a group number
- [ ] 1.2 Refuse unknown numbers, reversed ranges, all-ticked scopes and `--tasks` with `--all` or several changes, before making a worktree
- [ ] 1.3 Test: Run one group
- [ ] 1.4 Test: Range across groups
- [ ] 1.5 Test: Next group
- [ ] 1.6 Test: Unknown task
- [ ] 1.7 Test: Scope already done
- [ ] 1.8 Test: Scope with run all

## 2. Scoped runs in the engine (task-scope)

- [ ] 2.1 Add `scope` (as typed, task numbers) to `run.json`; `resume` and `rerun` keep it
- [ ] 2.2 `run` with a different scope on a ready, stopped, blocked or failed run starts again from the first step: round up, rejections cleared, same worktree; same scope behaves as today
- [ ] 2.3 After the last step's `done`, check the scope's ticks in the worktree; `blocked` with `scope tasks not ticked: …` or a missing number
- [ ] 2.4 A dependency's ready scoped run counts as met only when all its tasks are ticked; queued reason shows the scope
- [ ] 2.5 Test: Second scope while running
- [ ] 2.6 Test: Resume keeps the scope
- [ ] 2.7 Test: Next group after a ready one (round keeps counting, a late result from the old round is ignored)
- [ ] 2.8 Test: Rest of the change
- [ ] 2.9 Test: Ticks keep the next scope approved
- [ ] 2.10 Test: Design edited during a scope
- [ ] 2.11 Test: Scope ticked
- [ ] 2.12 Test: Task left unticked
- [ ] 2.13 Test: Dependency ready for one group

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
- [ ] 6.3 Archive after `add-herdr-board`, once the implementation is accepted; fold the board, history and mod requirements into `board-data`, `run-history` and `herdr-board`
