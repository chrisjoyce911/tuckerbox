# Spec Delta

## ADDED Requirements

### Requirement: Override allow-list is a repository rule
`[overrides]` SHALL be read only from repository config, so every machine allows the same change overrides. `tuckerbox check` SHALL fail when machine config sets it, and name the key. A permission profile name in `[overrides] permissions` SHALL grant nothing by itself: machine config still decides what each profile allows.

#### Scenario: Machine config sets the allow-list
- **WHEN** machine config contains `[overrides] agents = ["codex"]`
- **THEN** `tuckerbox check` exits non-zero and says `[overrides]` belongs in repository config

#### Scenario: Allowed profile not defined on the machine
- **WHEN** repository config has `[overrides] permissions = ["edit"]` and machine config defines no profile `edit`
- **THEN** the list entry alone does not fail `tuckerbox check`, but a change that sets `permissions = "edit"` fails it on that machine, naming the missing profile as for a repository step
