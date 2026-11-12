## ADDED Requirements

### Requirement: Machine-only forge credentials
Forge tokens and where they come from (`[forges."<host>"]` with `token_env` and `borrow_cli_token`) SHALL be honoured only from machine config. A repository config that holds a token, a token variable or `borrow_cli_token` SHALL fail validation and say these belong in machine config.

#### Scenario: Token in repository config
- **WHEN** a repository's `.tuckerbox/config.toml` contains `[forge] token = "..."`
- **THEN** `tuckerbox check` exits non-zero, names the key, says tokens belong in machine config, and does not print the value

### Requirement: Ship mode is a repository rule
`ship = "mr"` and `ship = "merge"`, and the `[shipping]` and `[forge]` tables, SHALL be read only from repository config, so every machine pushes and merges the same way. Machine config MAY set `ship = "local"` as a default for repositories that set no mode. `tuckerbox check` SHALL fail when machine config sets `mr`, `merge`, `[shipping]` or `[forge]`.

#### Scenario: Machine config asks to merge
- **WHEN** machine config contains `ship = "merge"`
- **THEN** `tuckerbox check` exits non-zero and says `ship = "merge"` belongs in repository config

#### Scenario: Shipping keys are checked
- **WHEN** repository config sets `ship = "mr"` and `[shipping] fix_ci = "fixci"` with no step `fixci`
- **THEN** `tuckerbox check` exits non-zero and names the key and the unknown step
