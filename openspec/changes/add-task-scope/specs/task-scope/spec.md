# Spec Delta

## Purpose

Defines how a run works on part of an OpenSpec change: choosing a scope of tasks, how the scope fits one run per change and approval, when a scoped run is ready, and where the scope shows.

## ADDED Requirements

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
When a scoped run becomes `ready`, Tuckerbox SHALL record in its run state whether every task of the change was ticked, in the same run state write that makes it `ready`, under the change's lock. Starting another scope SHALL clear the record. A run without a scope SHALL count as complete.

#### Scenario: Last group ready
- **WHEN** review reports `done` for scope `2` and every task of the change is ticked
- **THEN** the run state is `ready` and records the change as complete, in one write

#### Scenario: Earlier group ready
- **WHEN** review reports `done` for scope `1` and group 2 is not ticked
- **THEN** the run state is `ready` and records the change as not complete

### Requirement: Dependencies wait for the whole change
With `ship = "local"`, a dependency's ready run SHALL count as met only when its run state records the change as complete. A dependent SHALL judge this from that one read of the run state, never from the dependency's tasks.md. Until then it SHALL wait, with the dependency's state and scope as its reason. Model: `formal/scope.tla`, `DependentSeesReviewedWork`.

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

### Requirement: Scope in status and history
`tuckerbox status` SHALL show a run's scope, and `--json` SHALL add `scope` with the scope as typed and its task numbers. History events `started`, `resumed`, `rerun`, `ready` and `blocked` SHALL carry the scope. A new event `scope_started` SHALL record a run starting again for a new scope, with the old and new scopes.

#### Scenario: Next scope in history
- **WHEN** a ready run with scope `1` is started again with `--tasks 2`
- **THEN** its history has `scope_started` with old scope `1` and new scope `2`, then `queued`

#### Scenario: Status of a scoped run
- **WHEN** a dashboard reads `tuckerbox status --json` while `add-login` runs with scope `2.1-2.2`
- **THEN** its run has `scope` with `2.1-2.2` and tasks `2.1` and `2.2`, and the format `version` is unchanged

### Requirement: Scope on the board
A board card with a scoped run SHALL include its `scope`, and each round in card detail SHALL include the scope it ran with. Card detail SHALL list the task groups, each with its number, title, and ticked and total task counts.

#### Scenario: Groups in card detail
- **WHEN** a dashboard asks for the detail of a change whose group 1 has 3 of 3 tasks ticked and group 2 has 0 of 2
- **THEN** the detail lists group 1 with 3 of 3 and group 2 with 0 of 2, each with its title

### Requirement: Run group from the board
Card detail SHALL offer **Run group…** wherever it offers Start, and on Ready cards whose change still has unticked tasks. It SHALL list only groups with unticked tasks. Choosing one SHALL run `tuckerbox run <change> --tasks <group>` on the card's machine, and the board SHALL show the command's error if it fails.

#### Scenario: Run the next group from a Ready card
- **WHEN** a Ready card's run had scope `1` and the user picks group 2 under Run group…
- **THEN** the board runs `tuckerbox run <change> --tasks 2`, and the card leaves Ready on the next board read

#### Scenario: Nothing left to run
- **WHEN** every task of a Ready card's change is ticked
- **THEN** its detail offers no Run group…

#### Scenario: Run group refused
- **WHEN** the user picks a group and the run gate refuses because the approval is stale
- **THEN** the board shows the command's message and the card stays where it was
