# Spec Delta

## MODIFIED Requirements

### Requirement: Parallel limit
Tuckerbox SHALL run at most `max_parallel` changes at once per repository on one machine. It SHALL queue further changes and start them in admission order (see change-priority) as slots free up. At most one run per change SHALL be active on a machine at any time. This holds even when several `tuckerbox` processes start runs at once. Model: `formal/runs.tla`, invariants `OneRunPerChange` and `WithinLimit`.

#### Scenario: More changes than slots
- **WHEN** `max_parallel = 2` and the user runs `tuckerbox run --all` with four valid changes
- **THEN** two runs start, two are `queued`, and each queued run starts when a running one ends or pauses

#### Scenario: Change already running
- **WHEN** a change already has an active run and the user starts it again
- **THEN** Tuckerbox refuses and points to the active run

#### Scenario: Unranked changes keep request order
- **WHEN** `max_parallel = 1`, no change has a priority, and the user starts A, then B, then C
- **THEN** B starts before C
