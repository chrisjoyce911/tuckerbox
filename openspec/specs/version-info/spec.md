# version-info Specification

## Purpose
Defines what `tuckerbox version` reports, for people and for tools.

## Requirements

### Requirement: Version as JSON
`tuckerbox version --json` SHALL print one JSON object with the string fields `version`, `commit`, `go`, `os` and `arch`, followed by a newline, and exit 0. Plain `tuckerbox version` SHALL keep its current text output. Any other argument SHALL be an error with exit code 2.

#### Scenario: JSON output
- **WHEN** a user runs `tuckerbox version --json`
- **THEN** stdout is a single JSON object with `version`, `commit`, `go`, `os` and `arch`, and the exit code is 0

#### Scenario: Unknown flag
- **WHEN** a user runs `tuckerbox version --yaml`
- **THEN** the exit code is 2 and stderr names the unknown argument
