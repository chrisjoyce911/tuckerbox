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
