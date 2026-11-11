# Spec Delta

## Purpose

Defines the event history every Tuckerbox run keeps, so people and dashboards can see how a change reached its current state.

## ADDED Requirements

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
