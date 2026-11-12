# Spec Delta

## Purpose

Defines how people and other programs see what Tuckerbox runs are doing on a machine, so dashboards such as a Herdr panel can be built on it.

## ADDED Requirements

### Requirement: Status listing
`tuckerbox status` SHALL list every known run on the machine across repositories. For each run it SHALL show:
- repository and change id
- state: `queued`, `running`, `blocked`, `stopped`, `failed` or `ready`
- current step and round
- when the state last changed, in the machine's local time zone
- the blocked or failed reason, if any

#### Scenario: Mixed runs
- **WHEN** one change is running its review step and another is blocked
- **THEN** both appear, the blocked one with its reason

### Requirement: Machine-readable status
`tuckerbox status --json` SHALL print the same information as one JSON document, with timestamps in RFC 3339 including the offset, and a `version` field for the format. Fields SHALL only be added within a version, never renamed or removed.

#### Scenario: Dashboard reads status
- **WHEN** a dashboard runs `tuckerbox status --json`
- **THEN** it receives a JSON document with `version` and a `runs` array, and exit status 0 even when there are no runs
