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

### Requirement: Agent backend is machine config
`agent_backend` SHALL be read only from machine config, because whether herdr-core is installed and running is a fact about the machine. `tuckerbox check` SHALL fail when repository config sets it, and say it belongs in machine config.

#### Scenario: Repository sets the backend
- **WHEN** a repository's `.tuckerbox/config.toml` contains `agent_backend = "terminal"`
- **THEN** `tuckerbox check` exits non-zero and says `agent_backend` belongs in machine config

### Requirement: Every key has one scope
Each config key SHALL have one scope: `repo` (repository config only), `machine` (machine config only) or `either` (a machine default the repository overrides). `tuckerbox check`, `tuckerbox config` and the Settings view SHALL take scopes from one table, so they never disagree about where a key belongs.

#### Scenario: Scopes agree
- **WHEN** `tuckerbox config show --json` gives a key scope `machine`
- **THEN** `tuckerbox check` fails for a repository config that sets that key, and names it

### Requirement: Stored forge tokens
Tuckerbox SHALL read stored forge tokens from `credentials.toml` in the machine config folder, as `[forges."<host>"] token`, and from no other file. It SHALL try that token after `token_env` and before the default variables. This adds to `forge-access`'s token order once `add-forge-shipping` is archived.

#### Scenario: Stored token used first
- **WHEN** `credentials.toml` holds a token for `github.com` and `GITHUB_TOKEN` is also set
- **THEN** Tuckerbox uses the stored token, and `doctor` reports its source as the credentials file

#### Scenario: Token in machine config
- **WHEN** machine `config.toml` contains `[forges."github.com"] token = "..."`
- **THEN** `tuckerbox check` exits non-zero, says tokens belong in the credentials file through `tuckerbox config set`, and does not print the value

### Requirement: Credentials file is private
On macOS and Linux, Tuckerbox SHALL refuse to read `credentials.toml` when its group or others can read it. It SHALL say how to fix the file's mode and SHALL NOT print its contents.

#### Scenario: File readable by others
- **WHEN** `credentials.toml` has mode 0644
- **THEN** `tuckerbox check` exits non-zero, names the file, says to run `chmod 600` on it, and uses no token from it
