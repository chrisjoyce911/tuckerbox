# Spec Delta

## Purpose

Defines the machine-readable board for one repository, derived from OpenSpec and Tuckerbox run state, which any dashboard can display.

## ADDED Requirements

### Requirement: Board document
`tuckerbox board --json` run in a repository SHALL print one JSON document with `version`, the repository's remote URL (or its path if it has no remote), the machine name, whether the repository requires approval, the ordered `columns`, and a `cards` array. Times SHALL use the machine’s local time zone and include its offset. Format versioning SHALL follow the same rules as `tuckerbox status --json`.

#### Scenario: Repository without runs
- **WHEN** a repository has three active changes, none approved and none ever run
- **THEN** the document has three cards, all in the Proposed column

#### Scenario: Not a Tuckerbox repository
- **WHEN** `tuckerbox board --json` runs in a folder outside a git repository, or in a repository without `.tuckerbox/config.toml`
- **THEN** it exits non-zero and prints a document with `version` and `error` holding `code` `not_a_repository` and a message, so a dashboard can offer `tuckerbox init`

### Requirement: Derived columns
Columns SHALL be derived from OpenSpec, approval and run state, never stored. The order SHALL be Proposed, Approved, Queued, one column per workflow step in workflow order, Ready, then Done. When the repository sets `require_approval = false`, there SHALL be no Approved column.

#### Scenario: Repository with three steps
- **WHEN** the workflow's steps are plan, build and review and approval is required
- **THEN** the columns are Proposed, Approved, Queued, Plan, Build, Review, Ready, Done

#### Scenario: Approval not required
- **WHEN** the workflow's steps are plan, build and review and `require_approval = false`
- **THEN** the columns are Proposed, Queued, Plan, Build, Review, Ready, Done

### Requirement: Column placement
An active change with no run SHALL be in Approved when its approval state is `approved` and approval is required, else in Proposed. A run waiting for a slot or dependencies SHALL be in Queued. A running, stopped, blocked or failed run SHALL be in its current step's column. A finished, unarchived run SHALL be in Ready. The 20 most recently archived changes SHALL be in Done.

#### Scenario: Approved change
- **WHEN** a change with no run has approval state `approved`
- **THEN** its card is in the Approved column

#### Scenario: Stale approval
- **WHEN** a change with no run was approved and its proposal was edited afterwards
- **THEN** its card is in the Proposed column with approval state `stale`

#### Scenario: Blocked review
- **WHEN** a run is blocked in its review step because the review limit was reached
- **THEN** its card is in the Review column with state `blocked` and reason `review limit reached`

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
- **WHEN** change B depends on change A, which is in Build
- **THEN** B's card lists A with column Build

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
A run whose step is empty or no longer in the workflow SHALL stay visible in Queued, retaining its run state and reporting a per-card error.

#### Scenario: Removed workflow step
- **WHEN** a blocked run names a step that the workflow no longer contains
- **THEN** its card remains blocked, is in Queued and includes an error naming the missing step

### Requirement: Round lifecycle
Round detail SHALL include stopped and crashed rounds with result `stopped` and an end time. Admission events alone SHALL NOT create rounds before work starts or a step result is recorded.

#### Scenario: Stopped round
- **WHEN** a run is stopped or its process crashed after a step started
- **THEN** the round has result `stopped` and an end time, and admission events alone do not create empty rounds
