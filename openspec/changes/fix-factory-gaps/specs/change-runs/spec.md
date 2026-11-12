# Spec Delta

## MODIFIED Requirements

### Requirement: Stop, resume and re-run
`tuckerbox stop <change>` SHALL end the active step's agent and mark the run `stopped`. On a run with no agent running (`queued`, `blocked`, `failed` or crashed) it SHALL mark the run `stopped` and exit 0. `tuckerbox resume <change>` SHALL continue a `stopped` or `blocked` run from its current step. `tuckerbox rerun <change> <step>` SHALL start again from the named step. `tuckerbox close <change>` SHALL end a run as `run-closing` says. None of these SHALL alter files in the worktree.

#### Scenario: Resume after a stop
- **WHEN** a run was stopped during its review step and the user runs `tuckerbox resume`
- **THEN** the review step starts again in the same worktree

#### Scenario: Stop a blocked run
- **WHEN** `add-login`'s run is `blocked` and the user runs `tuckerbox stop add-login`
- **THEN** the run is `stopped`, the command exits 0, and nothing in the worktree changes
