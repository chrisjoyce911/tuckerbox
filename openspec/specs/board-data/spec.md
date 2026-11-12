# board-data Specification

## Purpose
Defines the machine-readable board for one repository, derived from OpenSpec and Tuckerbox run state, which any dashboard can display.

## Requirements

### Requirement: Board document
`tuckerbox board --json` run in a repository SHALL print one JSON document with `version`, the repository's remote URL (or its path if it has no remote), the machine name, whether the repository requires approval, the ordered `columns`, workflow step ids in an ordered `steps` array, and a `cards` array. Times SHALL use the machine’s local time zone and include its offset. Format versioning SHALL follow the same rules as `tuckerbox status --json`.

#### Scenario: Repository without runs
- **WHEN** a repository has three active changes, none approved and none ever run
- **THEN** the document has three cards, all in the Proposed column

#### Scenario: Not a Tuckerbox repository
- **WHEN** `tuckerbox board --json` runs in a folder outside a git repository, or in a repository without `.tuckerbox/config.toml`
- **THEN** it exits non-zero and prints a document with `version` and `error` holding `code` `not_a_repository` and a message, so a dashboard can say there is no Tuckerbox config

### Requirement: Derived columns
Columns SHALL be derived from OpenSpec, approval and run state, never stored. `[board] columns` SHALL select `compact` (default) or `steps`. Compact order SHALL be Proposed, Approved, In progress, Ready, Done. Steps order SHALL be Proposed, Approved, Queued, one column per workflow step in workflow order, Ready, Done. Approved SHALL appear only when `require_approval` is true. Column ids SHALL remain `proposed`, `approved`, `in_progress`, `queued`, `step:<id>`, `ready`, `done` as applicable.

#### Scenario: Compact default
- **WHEN** the workflow's steps are plan, build and review and no board layout is configured
- **THEN** the columns are Proposed, Approved, In progress, Ready, Done

#### Scenario: Repository with three steps
- **WHEN** `board.columns = "steps"`, workflow steps are plan, build and review and approval is required
- **THEN** the columns are Proposed, Approved, Queued, Plan, Build, Review, Ready, Done

#### Scenario: Approval not required
- **WHEN** `require_approval = false`
- **THEN** neither layout includes Approved; compact has Proposed, In progress, Ready, Done, while steps has Proposed, Queued, each workflow step, Ready, Done

### Requirement: Column placement
Without a run, an approved change SHALL be in Approved when approval is required, else Proposed. Compact SHALL group queued, running, stopped, blocked and failed runs in In progress. Steps SHALL place queued runs in Queued and other runs in their step column. A run's `badge` SHALL be `queued` or its step; other badges SHALL be empty. Finished runs SHALL be Ready and the 20 latest archives Done. Plain board output SHALL use the same placement and badges.

#### Scenario: Approved change
- **WHEN** a change with no run has approval state `approved` and approval is required
- **THEN** its card is in the Approved column in either layout

#### Scenario: Stale approval
- **WHEN** a change with no run was approved and its proposal was edited afterwards
- **THEN** its card is in the Proposed column with approval state `stale`

#### Scenario: Blocked review
- **WHEN** a run is blocked in review because the review limit was reached
- **THEN** its card is in In progress in compact layout or Review in steps layout, with badge `review`, state `blocked` and reason `review limit reached`

#### Scenario: Queued run
- **WHEN** a run waits for dependencies or a parallel slot, even if its recorded step is build
- **THEN** its card is in In progress in compact layout or Queued in steps layout, with badge `queued`

#### Scenario: Workflow runs
- **WHEN** runs are running, stopped or failed at any workflow step
- **THEN** each is in In progress in compact layout or its current step's column in steps layout, with the step as its badge

### Requirement: Card summary
Each card SHALL include the change id, a title (`title` from the change's `tuckerbox.toml`, else the change id with dashes as spaces; the proposal template's `# Proposal` heading is the same in every change, so it is not used), task progress from tasks.md, state and reason, current step, agent and round, and when the state last changed.

#### Scenario: Change without a title
- **WHEN** change `add-login-page` has no `title` in its `tuckerbox.toml`
- **THEN** its card's title is `add login page`

### Requirement: Card approval
Each card SHALL include its approval state (`approved`, `stale` or `none`), who approved and when, read from the folder where edits happen. It SHALL include `committed: false` when the approval or any hashed file has uncommitted changes there, because a run judges the committed base branch.

#### Scenario: Approved but not committed
- **WHEN** a change was just approved in the repository checkout and nothing was committed
- **THEN** its card has approval state `approved` with `committed: false`

### Requirement: Card location and dependencies
Each card with a run SHALL include its branch, worktree path and machine. Every card SHALL include the folder where edits happen, and list its dependencies with the column each is in.

#### Scenario: Waiting on a dependency
- **WHEN** change B depends on change A, which is running build
- **THEN** B's card lists A with column `in_progress` in compact layout or `step:build` in steps layout

### Requirement: Card detail
`tuckerbox board --json --change <id>` SHALL also include the proposal text, the change's spec deltas by capability, the task list with each task's number and checked state, each round with its result and findings, and the run history.

#### Scenario: Card detail
- **WHEN** a dashboard asks for the detail of a change that was rejected once
- **THEN** the document includes both rounds, the rejection's findings and the full history

#### Scenario: Task numbers
- **WHEN** a dashboard asks for the detail of a change whose task 2.1 is ticked
- **THEN** the task list has an entry with number `2.1` and checked `true`

### Requirement: Partial board reads
An unreadable change SHALL retain its card with an optional string `error` describing the failure and any fields already read. It SHALL NOT prevent other cards from being returned. Repository-wide failures SHALL still return a document error. Worktrees and their run-state directories SHALL be discovered at most once each per board read.

#### Scenario: Broken change metadata
- **WHEN** one active or archived change has malformed metadata or a missing worktree/change folder
- **THEN** its card contains an error and the other cards remain available

### Requirement: Archived approvals
Done cards SHALL show approval state `none`, blank approval metadata and `committed: true`, without approval hashing or git commit-status checks.

#### Scenario: Archived approval
- **WHEN** an archived change retains an old approval table
- **THEN** its card is in Done with approval state `none` and no approval or commit-status checks are run

### Requirement: Obsolete run steps
A run whose step is empty or no longer in the workflow SHALL stay visible in In progress (compact) or Queued (steps), with badge `queued`, retaining its run state and reporting a per-card error.

#### Scenario: Removed workflow step
- **WHEN** a blocked run names a step that the workflow no longer contains
- **THEN** its card remains blocked, is in In progress (compact) or Queued (steps) and includes an error naming the missing step

### Requirement: Round lifecycle
Round detail SHALL include stopped and crashed rounds with result `stopped` and an end time. Admission events alone SHALL NOT create rounds before work starts or a step result is recorded.

#### Scenario: Stopped round
- **WHEN** a run is stopped or its process crashed after a step started
- **THEN** the round has result `stopped` and an end time, and admission events alone do not create empty rounds

### Requirement: Scope on the board
A board card with a scoped run SHALL include its `scope`, and each round in card detail SHALL include the scope it ran with. Card detail SHALL list the task groups, each with its number, title, and ticked and total task counts.

#### Scenario: Groups in card detail
- **WHEN** a dashboard asks for the detail of a change whose group 1 has 3 of 3 tasks ticked and group 2 has 0 of 2
- **THEN** the detail lists group 1 with 3 of 3 and group 2 with 0 of 2, each with its title

### Requirement: Priority on the board
Each active card SHALL include its `priority` value, or none, and its `position` in priority order. The Proposed, Approved and In progress columns, and in the steps layout Queued and each step column, SHALL list cards in priority order. Ready and Done SHALL keep their order. Positions SHALL come from the board's own machine.

#### Scenario: Board order
- **WHEN** two Approved cards, A with priority 2000 and B with 1000, are read
- **THEN** B is listed before A, with positions in priority order
