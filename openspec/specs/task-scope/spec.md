# task-scope Specification

## Purpose
Defines how a run works on part of an OpenSpec change: choosing a scope of tasks, how the scope fits one run per change and approval, and when a scoped run is ready. Where the scope shows is in `run-history` (`Scope in status and history`), `board-data` (`Scope on the board`) and `herdr-board` (`Run group from the board`).

## Requirements

### Requirement: Choosing a scope
`tuckerbox run <change> --tasks <scope>` SHALL run the change with a task scope: a group (`2`), a task (`2.3`), a range of tasks in tasks.md order (`2.1-2.4`), or `next`, the first group with an unticked task. The scope SHALL be judged, and `next` resolved, in the tasks.md the run will use. It SHALL refuse, and create no worktree or run, for a number tasks.md doesn't have, a range that ends before it starts, or a scope whose tasks are all ticked.

#### Scenario: Run one group
- **WHEN** the user runs `tuckerbox run add-login --tasks 2` and group 2 has tasks 2.1 to 2.3
- **THEN** the run starts with scope `2`, covering tasks 2.1, 2.2 and 2.3

#### Scenario: Range across groups
- **WHEN** the user runs `tuckerbox run add-login --tasks 1.3-2.1`
- **THEN** the scope covers 1.3, every later task in group 1, and 2.1

#### Scenario: Next group
- **WHEN** group 1 is all ticked, group 2 has an unticked task, and the user runs `tuckerbox run add-login --tasks next`
- **THEN** the run starts with scope `2`, and status and history show `2`, not `next`

#### Scenario: Unknown task
- **WHEN** the user runs `tuckerbox run add-login --tasks 7` and tasks.md has no group 7
- **THEN** it refuses, says tasks.md has no group 7, and creates no worktree

#### Scenario: Scope already done
- **WHEN** every task in group 1 is ticked and the user runs `tuckerbox run add-login --tasks 1`
- **THEN** it refuses and says the tasks in scope `1` are all ticked

### Requirement: One change per scoped run
`--tasks` SHALL take exactly one change. With `--all` or several changes, `tuckerbox run` SHALL refuse and start nothing.

#### Scenario: Scope with run all
- **WHEN** the user runs `tuckerbox run --all --tasks 1`
- **THEN** it refuses, says `--tasks` takes one change, and starts nothing

### Requirement: The scope belongs to the change's run
A scope SHALL be part of the change's one run, kept in its run state. A change SHALL NOT have two active runs, whatever their scopes. `tuckerbox resume` and `tuckerbox rerun` SHALL keep the run's scope. A run without a scope SHALL cover the whole change, as before.

#### Scenario: Second scope while running
- **WHEN** `add-login` is running with scope `1` and the user runs `tuckerbox run add-login --tasks 2`
- **THEN** it refuses and points to the active run, as for any change already running

#### Scenario: Resume keeps the scope
- **WHEN** a run with scope `2` was stopped in its review step and the user runs `tuckerbox resume add-login`
- **THEN** the review step starts again with scope `2`

### Requirement: Starting the next scope
When the change's run is ready, stopped, blocked or failed, `tuckerbox run` with a different scope SHALL start that run again from the first workflow step with the new scope, in the same worktree and branch. Its round SHALL keep counting up, and its rejection count SHALL start again. No `--tasks` means the whole change. With the same scope, `run` SHALL behave as it does today.

#### Scenario: Next group after a ready one
- **WHEN** `add-login`'s run with scope `1` is ready after round 3 and the user runs `tuckerbox run add-login --tasks 2`
- **THEN** the first step starts in round 4 with scope `2`, in the same worktree

#### Scenario: Rest of the change
- **WHEN** `add-login`'s run with scope `1` is ready and the user runs `tuckerbox run add-login`
- **THEN** the first step starts again with no scope, covering the whole change

### Requirement: Approval covers the whole change
A scope SHALL NOT narrow approval. The run gate SHALL judge the whole change, as it does without a scope. Starting the next scope SHALL be gated again, so text that changed during an earlier scope must be approved again first.

#### Scenario: Ticks keep the next scope approved
- **WHEN** a run with scope `1` ticked tasks 1.1 to 1.3 and changed nothing else in the change's files
- **THEN** `tuckerbox run add-login --tasks 2` passes the run gate

#### Scenario: Design edited during a scope
- **WHEN** the build step of scope `1` edited design.md and the user runs `tuckerbox run add-login --tasks 2`
- **THEN** it refuses because the approval is stale, and the run stays ready

### Requirement: Ready when the scope is ticked
When the last step of a scoped run reports `done`, Tuckerbox SHALL read tasks.md in the worktree. The run SHALL be `ready` only if every task in its scope is ticked. Otherwise it SHALL be `blocked`, with a reason naming each unticked task, or a scope task tasks.md no longer has.

#### Scenario: Scope ticked
- **WHEN** review reports `done` for a run with scope `2` and tasks 2.1 to 2.3 are ticked
- **THEN** the run is `ready`

#### Scenario: Task left unticked
- **WHEN** review reports `done` for a run with scope `2` and task 2.3 is not ticked
- **THEN** the run is `blocked` with reason `scope tasks not ticked: 2.3`

### Requirement: Recording a complete change
When a run becomes `ready`, Tuckerbox SHALL record in its run state whether the change is complete: every task ticked except ship tasks. It SHALL do so in the same run state write that makes it `ready`, under the change's lock. Starting another scope SHALL clear the record.

#### Scenario: Last group ready
- **WHEN** review reports `done` for scope `2` and every task of the change is ticked
- **THEN** the run state is `ready` and records the change as complete, in one write

#### Scenario: Earlier group ready
- **WHEN** review reports `done` for scope `1` and group 2 is not ticked
- **THEN** the run state is `ready` and records the change as not complete

### Requirement: Dependencies wait for the whole change
With `ship = "local"`, a dependency's ready run SHALL count as met when it has no scope, as before, or when its run state records the change as complete. A dependent SHALL judge this from that one read of the run state, never from the dependency's tasks.md. Until then it SHALL wait, with the dependency's state and scope as its reason. Model: `formal/scope.tla`, `DependentSeesReviewedWork`.

#### Scenario: Dependency ready for one group
- **WHEN** change B depends on A, and A's run is ready with scope `1` and recorded as not complete
- **THEN** B stays `queued` with reason `waiting on A (ready, tasks 1)`

#### Scenario: Next group ticked while the dependent checks
- **WHEN** B depends on A, A is ready with scope `1` and not complete, then A starts scope `2` and its builder ticks every remaining task while B checks its dependencies
- **THEN** B stays `queued`, because A's run is not `ready` and complete in any single read

#### Scenario: Dependency complete
- **WHEN** change B depends on A, and A's run is ready with scope `2` and recorded as complete
- **THEN** B can start

#### Scenario: Ticks by hand don't release dependents
- **WHEN** A's run is ready with scope `1` and not complete, and a person ticks group 2 with `tuckerbox tasks`
- **THEN** B keeps waiting until a run of A becomes ready and complete

### Requirement: Ship tasks
A task whose text starts with `(ship)`, such as archiving the change, SHALL be done by shipping, not by a step. It SHALL NOT be part of a scope or of `next`, and SHALL NOT count when deciding whether a change is complete. A scope naming only ship tasks SHALL be refused. The step brief SHALL say not to tick them.

#### Scenario: Final group with an archive task
- **WHEN** group 6 has tasks 6.1, 6.2 and `6.3 (ship) Archive this change`, everything else is ticked, and a run with scope `6` ticks 6.1 and 6.2
- **THEN** the run records the change as complete, although 6.3 is unticked

#### Scenario: Scope of a ship task
- **WHEN** the user runs `tuckerbox run add-login --tasks 6.3` and 6.3 is a ship task
- **THEN** it refuses and says 6.3 is done by shipping

### Requirement: Partial runs don't ship
With a ship mode other than `local` (see add-forge-shipping), a scoped run whose change is not complete when its last step reports `done` SHALL become `ready` and SHALL NOT archive, push, open a merge request or merge. A run that is complete SHALL ship as that mode says. In those modes, a dependency SHALL count as met only once archived on the base branch, whatever its ticks.

#### Scenario: First group in merge mode
- **WHEN** `ship = "merge"` and a run with scope `1` finishes while group 2 is unticked
- **THEN** the run is `ready`, nothing is archived, pushed or merged, and no merge request is opened

#### Scenario: Last group in mr mode
- **WHEN** `ship = "mr"` and a run with scope `2` finishes with every task of the change ticked
- **THEN** the run enters `shipping`

#### Scenario: Dependents in merge mode
- **WHEN** `ship = "merge"`, B depends on A, every task of A is ticked, and A's run is `shipping` with its merge request not yet merged
- **THEN** B keeps waiting until A's archive is on the base branch

### Requirement: Incomplete whole-change runs
With a ship mode other than `local`, a run without a scope whose change is not complete when its last step reports `done` SHALL be `blocked`, with a reason naming the unticked tasks, and SHALL ship nothing.

#### Scenario: Whole-change run leaves a task
- **WHEN** `ship = "merge"`, a run without a scope finishes its last step, and task 3.2 is unticked
- **THEN** the run is `blocked` with reason `tasks not ticked: 3.2`, and nothing is archived or pushed

### Requirement: CI tasks
A task whose text starts with `(ci)`, or says it passes or is green on or in CI, SHALL be a CI task unless it is a ship task. With a ship mode other than `local`, an unticked CI task SHALL NOT block a scope's run and SHALL NOT count when deciding whether the change is complete. In `local` mode it SHALL count like any other task. Only with a ship mode other than `local` SHALL the step brief say not to tick CI tasks.

#### Scenario: CI task in local mode
- **WHEN** `ship = "local"` and a run with scope `1` leaves `1.2 (ci) Release archives build` unticked
- **THEN** the run is `blocked` with `scope tasks not ticked: 1.2`

#### Scenario: Brief in local mode
- **WHEN** `ship = "local"` and the run's scope has `1.2 Go tests pass on Linux, macOS and Windows CI`
- **THEN** the step brief doesn't say to leave 1.2 unticked, and a builder that ticks it makes the run `ready` and complete

### Requirement: Shipping ticks CI tasks
Shipping SHALL tick the change's CI tasks once CI is green on the merge request, commit them as `Tick <change> task N.M`, push that commit to the merge request, and wait for CI on it before reporting ready or merging. While CI is not green, CI tasks SHALL stay unticked.

#### Scenario: Final integration task passes on CI
- **WHEN** `ship = "mr"`, group 6 has `6.1 Wire it up` and `6.2 Go tests pass on Linux, macOS and Windows CI`, and a run with scope `6` ticks only 6.1
- **THEN** the run ships, and once CI is green on the merge request shipping ticks 6.2 in a commit `Tick add-login task 6.2` pushed to the merge request

#### Scenario: CI fails
- **WHEN** shipping a change with an unticked `(ci)` task and CI on the merge request is red
- **THEN** the `(ci)` task stays unticked

### Requirement: Change archived on its branch
When a run's last step is done and the change's folder is gone from its worktree while the worktree's `openspec/changes/archive` has the change's folder (`<date>-<change>`), the run SHALL read tasks from the archived `tasks.md`, SHALL count the change as complete, and SHALL ship as its mode says. Shipping SHALL record the archived folder and SHALL NOT archive the change again.

#### Scenario: Builder archived the change
- **WHEN** a builder moves `openspec/changes/add-priority` to `openspec/changes/archive/2026-11-12-add-priority` on the branch and review passes
- **THEN** the run is not blocked for scope tasks missing from `tasks.md`, and it is `ready` and complete in local mode or ships in `mr` and `merge` modes

#### Scenario: Not archived twice
- **WHEN** `shipping.archive = true` and the change is already archived on its branch
- **THEN** shipping doesn't run `openspec archive` and doesn't commit `Archive <change>`
