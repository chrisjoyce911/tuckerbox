# settings-view Specification

## Purpose
Defines the Settings view in the Herdr board mod: a person sees the effective configuration of a repository and of a machine, where each value comes from, and changes it through `tuckerbox config`.

## Requirements

### Requirement: Opening Settings
The board header SHALL offer Settings in the board window and in a docked pane, not in the drawer. Settings SHALL replace the columns with the Settings view for the repository and machine the board shows, and Back SHALL return to the columns.

#### Scenario: Open and go back
- **WHEN** the user clicks Settings in the board window, then Back
- **THEN** the window shows the Settings view for the same repository and machine, then the columns again

### Requirement: Settings data
The view SHALL read `tuckerbox config show --json` on the board's machine when opened, after each save, and when the person asks. It SHALL drop a read that finishes after the space or machine changed, as board-view's `Stale reads are dropped` rule does for the board.

#### Scenario: Space changed during a read
- **WHEN** the user switches to another space while the Settings read for the first space runs
- **THEN** the first space's settings are never shown in the view

### Requirement: Repository and machine sections
The view SHALL show a Repository section and a "This machine only" section. The machine section SHALL name the machine and say its values are neither committed nor shared. A key SHALL be editable only where its scope allows: `repo` keys in Repository, `machine` keys in the machine section, `either` keys in both.

#### Scenario: Machine-only key
- **WHEN** the user opens Settings on machine `studio`
- **THEN** permission profiles appear only under "This machine only (studio)", and the Repository section offers no way to set them

### Requirement: Values and sources
Each key SHALL show its effective value and a source badge: `repo`, `machine` or `default`. When a value overrides another file's value, the view SHALL show both.

#### Scenario: Overridden machine default
- **WHEN** machine config sets `max_parallel = 4` and repository config sets `2`
- **THEN** Repository shows `max_parallel` `2` with badge `repo` and notes the machine's `4`; the machine section shows `4` as its default

### Requirement: Setting groups
Repository SHALL group Workflow (steps in order, each with agent, role, model, effort, permissions, `on_reject` and `max_rounds`), Runs (`max_parallel`, `ship`, `require_approval`), Board, Autopilot, Change overrides, and Other for any other key. The machine section SHALL group Permission profiles, Forge tokens, Editor, Machines and Other. A group with no keys in this build SHALL be hidden.

#### Scenario: Workflow group
- **WHEN** repository config has `[workflow] steps = ["plan", "build", "review"]`
- **THEN** the Workflow group lists plan, build and review in that order, each with its settings and sources

### Requirement: Editing the workflow
The Workflow group SHALL let the person add a step (an id and an agent), remove one, and move one up or down. Each SHALL become pending edits to `workflow.steps` and `steps.<id>` keys.

#### Scenario: Reorder steps
- **WHEN** the user moves review above build and saves
- **THEN** the view runs `tuckerbox config set` with `workflow.steps=plan,review,build`

### Requirement: Pending edits and saving
Edits SHALL collect in a pending list and be saved together by one `tuckerbox config set ... --if-match <revision>` on the board's machine. The view SHALL check pending edits with `--dry-run --json`, show each problem by its field, and say where the save will go and why. Save SHALL be disabled while a pending edit adds a problem.

#### Scenario: Edit that adds a problem
- **WHEN** the user sets the review step's `on_reject` to `bulid`
- **THEN** the field shows that no step `bulid` exists, and Save is disabled

#### Scenario: Save as a pull request
- **WHEN** the user saves two pending repository edits and this machine has a token for the forge
- **THEN** the view runs one `tuckerbox config set --if-match`, shows the merge request link, and reloads

### Requirement: The view writes no files
The view SHALL change configuration only by running `tuckerbox config` commands, and SHALL show a command's error when it fails. It SHALL NOT write any file itself.

#### Scenario: Command refused
- **WHEN** a save fails because the checkout's config cannot be written
- **THEN** the view shows the command's message and keeps the pending edits

### Requirement: Save conflicts
When a save is refused as changed since read, the view SHALL keep the pending edits, reload, mark each pending key whose value changed meanwhile, and let the person save again.

#### Scenario: Someone saved first
- **WHEN** the user's save is refused because a terminal changed `max_parallel` since the view read it
- **THEN** the view reloads, keeps the pending edits, and marks `max_parallel` as changed by someone else

### Requirement: Tokens in the view
The Forge tokens group SHALL show each forge host with `set` or `not set` and the token's source, never a value. Set token SHALL open a Herdr pane on the board's machine running `tuckerbox config set --machine forges.<host>.token`, so the token goes only through that prompt.

#### Scenario: Set a token
- **WHEN** the user clicks Set token for `github.com`
- **THEN** a pane opens running the prompt, and after it closes the view reloads and shows the token as `set` from the credentials file

### Requirement: Broken config files
When `tuckerbox config show` reports a file it cannot parse, the view SHALL show the problem with the file and line, show no editable fields for that file, and offer Edit file, which opens a pane running `tuckerbox config edit`, with `--machine` for machine config.

#### Scenario: Syntax error in repository config
- **WHEN** repository config has a TOML syntax error
- **THEN** the Repository section shows the error and offers Edit file, and the machine section still works
