# Spec Delta

## ADDED Requirements

### Requirement: Board layout setting
`[board] columns` SHALL accept `"compact"` (default) or `"steps"`. It MAY be set in machine config as a default; a repository value SHALL override it under the existing machine-default precedence rules. `tuckerbox check` SHALL reject an unknown value with the source file and key `board.columns`.

#### Scenario: Default layout
- **WHEN** neither config sets `board.columns`
- **THEN** the board uses compact layout

#### Scenario: Machine default
- **WHEN** machine config sets `board.columns = "steps"` and repository config omits it
- **THEN** the board uses steps layout

#### Scenario: Repository override
- **WHEN** machine config sets `board.columns = "steps"` and repository config sets `board.columns = "compact"`
- **THEN** the board uses compact layout

#### Scenario: Unknown layout
- **WHEN** the effective `board.columns` value in either file is unknown, including an empty string
- **THEN** `tuckerbox check` exits non-zero and names that file, `board.columns` and the invalid value
