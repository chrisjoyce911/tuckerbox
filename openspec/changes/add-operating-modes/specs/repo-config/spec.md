# Spec Delta

## ADDED Requirements

### Requirement: Mode and limits are repository rules
`mode`, `[fleet]` (`max_runs`, `lease`) and `[team]` (`approvers`) SHALL be read only from repository config. `tuckerbox check` SHALL fail when machine config sets any of them, and name the key.

#### Scenario: Machine config sets a fleet limit
- **WHEN** machine config contains `[fleet] max_runs = 10`
- **THEN** `tuckerbox check` exits non-zero and says `fleet.max_runs` belongs in repository config

### Requirement: Machines are machine config
The machine list (`[machines.<name>]`) and `fleet_capacity` SHALL be read only from machine config. `tuckerbox check` SHALL fail when repository config sets them, and say they belong in machine config.

#### Scenario: Repository lists machines
- **WHEN** a repository's config contains `[machines.mini] ssh = "mini"`
- **THEN** `tuckerbox check` exits non-zero and says `machines` belongs in machine config
