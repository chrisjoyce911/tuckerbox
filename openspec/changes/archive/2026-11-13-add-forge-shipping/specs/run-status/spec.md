## MODIFIED Requirements

### Requirement: Status listing
`tuckerbox status` SHALL list every known run on the machine across repositories. For each run it SHALL show:
- repository and change id
- state: `queued`, `running`, `shipping`, `blocked`, `stopped`, `failed`, `ready` or `merged`
- current step and round
- when the state last changed, in the machine's local time zone
- the blocked or failed reason, if any
- for a run with a merge request: its link, CI state and ship round

#### Scenario: Mixed runs
- **WHEN** one change is running its review step and another is blocked
- **THEN** both appear, the blocked one with its reason

#### Scenario: Shipping run
- **WHEN** a run is `shipping` with its merge request open and CI pending on round 2
- **THEN** its line shows state `shipping`, the merge request link, CI `pending` and ship round 2

### Requirement: Machine-readable status
`tuckerbox status --json` SHALL print the same information as one JSON document, with timestamps in RFC 3339 including the offset, and a `version` field for the format. Fields SHALL only be added within a version, never renamed or removed. New state values MAY be added within a version, so readers SHALL accept a state value they do not know.

#### Scenario: Dashboard reads status
- **WHEN** a dashboard runs `tuckerbox status --json`
- **THEN** it receives a JSON document with `version` and a `runs` array, and exit status 0 even when there are no runs

#### Scenario: Shipping fields
- **WHEN** a dashboard reads status with a run in state `shipping`
- **THEN** that run has `mr` (link, number, state) and `ci` (state, commit) and `ship_round`, and the document's `version` is unchanged
