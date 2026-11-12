# run-history Specification

## Purpose
Defines the event history every Tuckerbox run keeps, so people and dashboards can see how a change reached its current state.

## Requirements

### Requirement: Append-only run history
Every run SHALL record each of these events as it happens:
- run started, queued, stopped, resumed, blocked, failed or ready
- step started
- step finished, with its result
- hand-off to the next step, with the step's summary
- rejection, with findings

Each event SHALL carry a timestamp with its offset, the step and round, the agent and the machine. Events SHALL never be rewritten or removed while the worktree exists.

#### Scenario: Rejection is recorded
- **WHEN** the review step rejects round 1 with two findings
- **THEN** the history has a rejection event for round 1 containing both findings, followed by a step-started event for the build step in round 2

#### Scenario: History survives a resume
- **WHEN** a run is stopped and later resumed
- **THEN** the history still has every earlier event, followed by stopped and resumed events

### Requirement: Reading history
`tuckerbox history <change>` SHALL print the run's events oldest first, with times in the machine's local time zone. `--json` SHALL print them as a JSON document with a `version` field, under the same rules as `tuckerbox status --json`.

#### Scenario: No run yet
- **WHEN** the user asks for the history of a change that has never run
- **THEN** Tuckerbox prints an empty history and exits 0

### Requirement: Scope in status and history
`tuckerbox status` SHALL show a run's scope, and `--json` SHALL add `scope` with the scope as typed and its task numbers. History events `started`, `resumed`, `rerun`, `ready` and `blocked` SHALL carry the scope. A new event `scope_started` SHALL record a run starting again for a new scope, with the old and new scopes.

#### Scenario: Next scope in history
- **WHEN** a ready run with scope `1` is started again with `--tasks 2`
- **THEN** its history has `scope_started` with old scope `1` and new scope `2`, then `queued`

#### Scenario: Status of a scoped run
- **WHEN** a dashboard reads `tuckerbox status --json` while `add-login` runs with scope `2.1-2.2`
- **THEN** its run has `scope` with `2.1-2.2` and tasks `2.1` and `2.2`, and the format `version` is unchanged
