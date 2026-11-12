# change-settings Specification

## Purpose
Defines how one OpenSpec change overrides the agent, model, effort and permission profile of its own workflow steps, within limits the repository sets, and how those overrides affect approval and runs.

## Requirements

### Requirement: Step overrides in the change sidecar
A change's `tuckerbox.toml` MAY hold `[steps.<step>]` tables with `agent`, `model`, `effort` and `permissions` (a permission profile name). `<step>` SHALL name a step defined in the repository's `[steps]`. Any other key in such a table SHALL be refused. The overrides travel with the change and are archived with it.

#### Scenario: Override one step
- **WHEN** change `add-login` has `[steps.build] agent = "codex"` and the repository allows `codex`
- **THEN** its build step runs Codex and its other steps run as the repository config says

#### Scenario: Unknown step
- **WHEN** change `add-login` has `[steps.biuld] agent = "codex"` and the repository defines no step `biuld`
- **THEN** `tuckerbox check` exits non-zero and names the change, the key and the unknown step

#### Scenario: Raw flags in an override
- **WHEN** change `add-login` has `[steps.build] args = ["--dangerously-skip-permissions"]`
- **THEN** `tuckerbox check` exits non-zero, names the key, and says permission flags belong in machine config

### Requirement: Preferred machines
A change's `tuckerbox.toml` MAY hold `machines`, a list of machine names the change prefers to run on, in order. Each name SHALL be a non-empty string. In solo mode it SHALL have no effect on where or whether the change runs. `add-operating-modes` uses it for placement.

#### Scenario: Machines in solo mode
- **WHEN** change `add-login` has `machines = ["mini"]` and the user runs it on machine `studio` in a solo repository
- **THEN** the run starts on `studio` and the card shows the preference `mini`

### Requirement: Override allow-list
Repository config `[overrides]` SHALL list what a change may choose: `agents`, `models` (a list per agent), `efforts` and `permissions` (profile names). A change override SHALL be accepted only when its value is listed. With no `[overrides]`, or a key missing from it, a change SHALL NOT override that key.

#### Scenario: Value not allowed
- **WHEN** `[overrides] agents = ["claude"]` and change `add-login` sets `[steps.build] agent = "codex"`
- **THEN** `tuckerbox check` exits non-zero and names the change, the key, the value and the allowed values

#### Scenario: No allow-list
- **WHEN** the repository has no `[overrides]` table and a change sets `[steps.review] effort = "low"`
- **THEN** `tuckerbox check` exits non-zero and says the repository allows no change overrides

#### Scenario: Model for another agent
- **WHEN** `[overrides] models = { codex = ["gpt-5.5"] }` and a change sets `model = "gpt-5.5"` on a step whose effective agent is `claude`
- **THEN** `tuckerbox check` exits non-zero and says `gpt-5.5` is not an allowed model for `claude`

### Requirement: Effective step settings
For each step and key, the effective value SHALL be the change override if set, else the repository step's value, else the machine step's value, else the agent's built-in default. A model or effort chosen for one agent SHALL NOT carry over when an override changes the step's agent: it SHALL come from the change or fall back to that agent's default.

#### Scenario: Change wins
- **WHEN** the repository sets `[steps.review] effort = "high"` and the change sets `[steps.review] effort = "medium"`
- **THEN** the review step runs with effort `medium`, recorded as coming from the change

#### Scenario: Agent changed without a model
- **WHEN** the repository's build step is `agent = "claude"` with `model = "opus"`, and the change sets only `[steps.build] agent = "codex"`
- **THEN** the build step runs Codex with Codex's default model, not `opus`

### Requirement: Grants stay with the machine
A change MAY only name a permission profile. The profile SHALL be honoured only if machine config defines it and allows the repository's remote, as for a repository step. A change SHALL NOT define a profile, add flags, or widen a profile's allowed remotes.

#### Scenario: Profile not granted here
- **WHEN** the repository allows profile `edit`, a change sets `[steps.build] permissions = "edit"`, and this machine's config has no profile `edit`
- **THEN** the run refuses to start the build step and names the missing profile, as for a repository step

### Requirement: Runs refuse invalid overrides
`tuckerbox run`, `resume` and `rerun` SHALL check the change's overrides against the repository config the run uses, and refuse a change whose overrides are not allowed, naming each. They SHALL judge the sidecar from the same files the run gate judges: the worktree, else the commit a new worktree is made from.

#### Scenario: Allow-list narrowed later
- **WHEN** change `add-login` sets `agent = "codex"` and the repository later removes `codex` from `[overrides] agents`
- **THEN** `tuckerbox run add-login` refuses, names the override, and creates no worktree

### Requirement: Setting a value
`tuckerbox set <change> <key>=<value>` SHALL set one key: `step.<step>.agent`, `step.<step>.model`, `step.<step>.effort`, `step.<step>.permissions`, or `machines` (comma-separated). It SHALL refuse a value the allow-list does not hold, keep the sidecar's other keys and comments, and not commit. It SHALL say the change counts elsewhere only once committed and pushed.

#### Scenario: Set an agent
- **WHEN** the user runs `tuckerbox set add-login step.build.agent=codex` and `codex` is allowed
- **THEN** the sidecar has `[steps.build] agent = "codex"`, its `depends_on` and `[approval]` lines are unchanged, and the output says nothing was committed

#### Scenario: Set a value not allowed
- **WHEN** the user runs `tuckerbox set add-login step.build.effort=max` and `max` is not in `[overrides] efforts`
- **THEN** it exits non-zero, lists the allowed efforts, and leaves the sidecar unchanged

### Requirement: Removing a value
`tuckerbox set <change> --unset <key>` SHALL remove that one override, and remove a `[steps.<step>]` table left empty. The step then SHALL use the repository's value again. Removing an override that is not set SHALL change nothing and exit 0.

#### Scenario: Back to the repository default
- **WHEN** change `add-login` sets `[steps.build] model = "sonnet"` and the user runs `tuckerbox set add-login --unset step.build.model`
- **THEN** the sidecar has no `[steps.build]` table and the build step uses the repository's model

### Requirement: Settings and active runs
`tuckerbox set` SHALL take the change's run lock while it writes, as `tuckerbox tasks` does, and refuse while the change has a queued or running run. It SHALL write where edits happen: the change's worktree if it has one, else the checkout. A stopped or blocked run SHALL use the new values from the next step it starts.

#### Scenario: Set during a run
- **WHEN** change `add-login` is running its build step and the user runs `tuckerbox set add-login step.review.model=opus`
- **THEN** it refuses, says the change is running, and leaves the sidecar unchanged

#### Scenario: Set while blocked
- **WHEN** a run is blocked in review and the user sets `step.review.effort=high` then resumes it
- **THEN** the resumed review step runs with effort `high`

### Requirement: Settings in run state and history
When a step starts, Tuckerbox SHALL record its effective agent, model, effort and profile name, and the source of each (`default`, `machine`, `repo` or `change`), in the run's state and in the step's `step_started` history event. It SHALL NOT record a profile's flags.

#### Scenario: History shows the override
- **WHEN** the build step starts with `agent = "codex"` from the change and the model from Codex's default
- **THEN** its `step_started` event has agent `codex` with source `change` and the model with source `default`

### Requirement: Settings that void approval
`tuckerbox approve` SHALL also write `settings_hash` to `[approval]`: a SHA-256 over every `agent` and `permissions` override in the sidecar, sorted by key. An approval SHALL be `stale` when either hash no longer matches. A missing `settings_hash` SHALL mean the hash of no overrides. This narrows `change-approval`'s `Approval hash` once `add-herdr-board` is archived.

#### Scenario: Agent changed after approval
- **WHEN** an approved change gets `tuckerbox set add-login step.build.agent=codex`
- **THEN** its approval state is `stale` and `tuckerbox run add-login` refuses

#### Scenario: Old approval without overrides
- **WHEN** a change approved before this change has an `[approval]` with no `settings_hash` and no step overrides
- **THEN** its approval state is still `approved`

### Requirement: Settings that keep approval
Changing a change's `model`, `effort` or `machines` SHALL NOT change either approval hash, so the approval state stays as it was.

#### Scenario: Model changed after approval
- **WHEN** an approved change gets `tuckerbox set add-login step.build.model=sonnet`
- **THEN** its approval state is still `approved`

### Requirement: Resume checks approved settings
With `require_approval = true`, `tuckerbox resume` and `tuckerbox rerun` SHALL refuse when the change's `settings_hash` no longer matches, and say which override changed since approval. A stale text hash alone SHALL NOT stop them, as today.

#### Scenario: Agent changed on a stopped run
- **WHEN** an approved change's run is stopped, the user sets `step.review.agent=codex`, then runs `tuckerbox resume add-login`
- **THEN** resume refuses, names `step.review.agent`, and says to approve the change again

#### Scenario: Plan step rewrote the design
- **WHEN** a plan step edited the design, making the text hash stale, and the run then blocked
- **THEN** `tuckerbox resume` continues the run
