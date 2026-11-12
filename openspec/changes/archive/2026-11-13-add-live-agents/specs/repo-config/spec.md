# Spec Delta

## ADDED Requirements

### Requirement: Agent backend is machine config
`agent_backend` SHALL be read only from machine config, because whether herdr-core is installed and running is a fact about the machine. `tuckerbox check` SHALL fail when repository config sets it, and say it belongs in machine config.

#### Scenario: Repository sets the backend
- **WHEN** a repository's `.tuckerbox/config.toml` contains `agent_backend = "terminal"`
- **THEN** `tuckerbox check` exits non-zero and says `agent_backend` belongs in machine config
