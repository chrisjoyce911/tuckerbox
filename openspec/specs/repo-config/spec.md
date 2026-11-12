# repo-config Specification

## Purpose
Defines where Tuckerbox finds a repository's workflow settings, how they combine with the machine's own defaults, and which settings only the machine may decide.

## Requirements

### Requirement: Repository config file
Tuckerbox SHALL read a repository's settings from `.tuckerbox/config.toml` at the repository root of the checkout it runs in. The file is committed with the repository, so every machine that checks out the same revision runs the same workflow.

#### Scenario: Config present
- **WHEN** a repository contains `.tuckerbox/config.toml`
- **THEN** Tuckerbox uses its workflows, steps, agents and limits for runs in that repository

#### Scenario: Config absent
- **WHEN** a repository has no `.tuckerbox/config.toml`
- **THEN** Tuckerbox refuses to start a run and says how to create one with `tuckerbox init`

### Requirement: Machine defaults
Tuckerbox SHALL read optional machine defaults from `config.toml` in the user's config directory (`$XDG_CONFIG_HOME/tuckerbox`, `~/.config/tuckerbox` on macOS and Linux, `%AppData%\tuckerbox` on Windows). A repository value SHALL override the machine value for the same key, except for keys reserved to the machine.

#### Scenario: Repo overrides a default
- **WHEN** the machine sets `max_parallel = 4` and the repository sets `max_parallel = 2`
- **THEN** at most two changes from that repository run at once

### Requirement: Machine-only permission settings
Agent permission settings (extra command-line flags, permission modes, sandbox and approval policies) SHALL be honoured only from machine config. A repository MAY name a permission profile; Tuckerbox SHALL use it only if the machine config defines a profile with that name and lists the repository's remote, or `*`, as allowed.

#### Scenario: Repository asks for an undefined profile
- **WHEN** a step names permission profile `unattended` and the machine config has no such profile
- **THEN** Tuckerbox refuses to start the run and names the missing profile

#### Scenario: Repository puts flags in its config
- **WHEN** a repository's config contains raw agent arguments
- **THEN** validation fails and says those arguments belong in machine config

### Requirement: Config validation
`tuckerbox check` SHALL validate the merged config and report every problem with its file and key before any run starts. Problems include unknown keys, unknown agents, loops that point to undefined steps, and steps unreachable from the first step.

#### Scenario: Loop to an undefined step
- **WHEN** a step declares `on_reject = "bulid"` and no step has that id
- **THEN** `tuckerbox check` exits non-zero and names the step, the key and the unknown id

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

### Requirement: Approval setting
The repository config key `require_approval` SHALL default to `true`. It SHALL be read only from the repository's `.tuckerbox/config.toml`, so every machine applies the same gate. `tuckerbox check` SHALL fail when machine config sets it, and name the key.

#### Scenario: Machine config sets the key
- **WHEN** machine config contains `require_approval = false`
- **THEN** `tuckerbox check` exits non-zero and says `require_approval` belongs in repository config

### Requirement: Override allow-list is a repository rule
`[overrides]` SHALL be read only from repository config, so every machine allows the same change overrides. `tuckerbox check` SHALL fail when machine config sets it, and name the key. A permission profile name in `[overrides] permissions` SHALL grant nothing by itself: machine config still decides what each profile allows.

#### Scenario: Machine config sets the allow-list
- **WHEN** machine config contains `[overrides] agents = ["codex"]`
- **THEN** `tuckerbox check` exits non-zero and says `[overrides]` belongs in repository config

#### Scenario: Allowed profile not defined on the machine
- **WHEN** repository config has `[overrides] permissions = ["edit"]` and machine config defines no profile `edit`
- **THEN** the list entry alone does not fail `tuckerbox check`, but a change that sets `permissions = "edit"` fails it on that machine, naming the missing profile as for a repository step
