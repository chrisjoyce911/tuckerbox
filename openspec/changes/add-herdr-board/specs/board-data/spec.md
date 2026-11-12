# Spec Delta

## Purpose

Defines the machine-readable board for one repository, derived from OpenSpec and Tuckerbox run state, which any dashboard can display.

## ADDED Requirements

### Requirement: Board document
`tuckerbox board --json` run in a repository SHALL print one JSON document with `version`, the repository's remote URL (or its path if it has no remote), the machine name, the ordered `columns`, and a `cards` array. Format versioning SHALL follow the same rules as `tuckerbox status --json`.

#### Scenario: Repository without runs
- **WHEN** a repository has three active changes and none has ever run
- **THEN** the document has three cards, all in the Proposed column

### Requirement: Derived columns
Columns SHALL be derived from OpenSpec and run state, never stored. The order SHALL be Proposed, Queued, one column per workflow step in workflow order, Ready, then Done.

#### Scenario: Repository with three steps
- **WHEN** the workflow's steps are plan, build and review
- **THEN** the columns are Proposed, Queued, Plan, Build, Review, Ready, Done

### Requirement: Column placement
An active change with no run SHALL be in Proposed. A run waiting for a slot or dependencies SHALL be in Queued. A running, stopped, blocked or failed run SHALL be in its current step's column. A finished, unarchived run SHALL be in Ready. The 20 most recently archived changes SHALL be in Done.

#### Scenario: Blocked review
- **WHEN** a run is blocked in its review step because the review limit was reached
- **THEN** its card is in the Review column with state `blocked` and reason `review limit reached`

### Requirement: Card summary
Each card SHALL include the change id, a title (`title` from the change's `tuckerbox.toml`, else the change id with dashes as spaces; the proposal template's `# Proposal` heading is the same in every change, so it is not used), task progress from tasks.md, state and reason, current step, agent and round, and when the state last changed.

#### Scenario: Change without a title
- **WHEN** change `add-login-page` has no `title` in its `tuckerbox.toml`
- **THEN** its card's title is `add login page`

### Requirement: Card location and dependencies
Each card with a run SHALL include its branch, worktree path and machine. Every card SHALL list its dependencies with the column each is in.

#### Scenario: Waiting on a dependency
- **WHEN** change B depends on change A, which is in Build
- **THEN** B's card lists A with column Build

### Requirement: Card detail
`tuckerbox board --json --change <id>` SHALL also include the proposal text, the task list with checked state, each round with its result and findings, and the run history.

#### Scenario: Card detail
- **WHEN** a dashboard asks for the detail of a change that was rejected once
- **THEN** the document includes both rounds, the rejection's findings and the full history
