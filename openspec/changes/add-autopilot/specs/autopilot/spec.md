# Spec Delta

## Purpose

Defines `tuckerbox autopilot`, the foreground loop that keeps starting approved OpenSpec changes on one machine, works through them group by group, hands finished work to shipping, and stops only for a person when a run needs one.

## ADDED Requirements

### Requirement: Running autopilot
`tuckerbox autopilot` SHALL run in the foreground for the repository it is started in, until interrupted. It SHALL NOT install or start a background service. It SHALL refuse to start when `require_approval` is false. Only one autopilot SHALL run per repository on a machine: a second SHALL refuse and name the first one's process id.

#### Scenario: Approval off
- **WHEN** the repository sets `require_approval = false` and the user runs `tuckerbox autopilot`
- **THEN** it exits non-zero, says autopilot needs approval to be required, and starts nothing

#### Scenario: Second autopilot
- **WHEN** autopilot is running for a repository and the user starts another in the same repository on the same machine
- **THEN** the second exits non-zero and names the first one's process id

### Requirement: Passes
Autopilot SHALL make a pass at start, every `[autopilot] interval` (60 seconds by default), and when a run on this machine ends. A pass SHALL fetch the base branch and look at the active changes in admission order. `--once` SHALL make one pass and exit. `--dry-run` SHALL print what a pass would start and start nothing.

#### Scenario: Dry run
- **WHEN** two approved changes have no run and the user runs `tuckerbox autopilot --once --dry-run`
- **THEN** it prints both changes in admission order and starts nothing

### Requirement: What a pass starts
A pass SHALL start a change only when it is approved, its dependencies are met, and it has no run, or a ready run whose change still has unticked tasks. It SHALL start changes in admission order until the repository's queued and running runs on this machine reach `max_parallel`.

#### Scenario: Within max_parallel
- **WHEN** `max_parallel = 2`, one run is running, and three approved changes have no run
- **THEN** the pass starts the first of the three in admission order, and only that one

#### Scenario: Unapproved change
- **WHEN** a change is in Proposed with no approval
- **THEN** no pass starts it

#### Scenario: Dependency not met
- **WHEN** approved change B depends on A, which is still running
- **THEN** the pass doesn't start B, and starts it on a later pass once A counts as met

### Requirement: Group by group
With `[autopilot] scope = "group"`, the default, autopilot SHALL start a change with `--tasks next`, and SHALL start a ready run whose change has unticked tasks again with `--tasks next`. With `scope = "change"`, it SHALL start whole changes. `tuckerbox check` SHALL reject any other value.

#### Scenario: Next group
- **WHEN** a change's run is ready with scope `1` and group 2 is unticked
- **THEN** the next pass starts it with `--tasks next`, which resolves to `2`

#### Scenario: Whole changes
- **WHEN** `scope = "change"` and an approved change has no run
- **THEN** the pass starts it with no scope

### Requirement: Partial runs don't ship
With `ship = "mr"` or `"merge"`, a run whose last step reports `done` while its change still has unticked tasks SHALL become `ready` without shipping. Once every task is ticked, it SHALL ship as `add-forge-shipping` says. This applies to every run, started by autopilot or not.

#### Scenario: First group in merge mode
- **WHEN** `ship = "merge"` and a run with scope `1` finishes while group 2 is unticked
- **THEN** the run is `ready`, nothing is archived or pushed, and the next pass starts group 2

#### Scenario: Last group in merge mode
- **WHEN** `ship = "merge"` and a run with scope `2` finishes with every task ticked
- **THEN** the run enters `shipping`, and archives, pushes and merges as forge shipping says

### Requirement: Autopilot never overrides a person
Autopilot SHALL NOT approve, unapprove, edit or tick, SHALL NOT resume a `blocked` run or one a person stopped, and SHALL NOT re-run a `failed` one. It SHALL leave a change alone while a person's edit holds its lock.

#### Scenario: Person stopped a run
- **WHEN** a person ran `tuckerbox stop add-login`
- **THEN** no pass resumes or restarts `add-login`

#### Scenario: Blocked run
- **WHEN** a run is `blocked` with reason `review limit reached`
- **THEN** no pass resumes it

### Requirement: Acting on what it saw
Each start or resume autopilot makes SHALL carry the run state it saw. Once it holds the change lock, the run engine SHALL go ahead only if the run is still in that state and the run gate passes, and otherwise SHALL change nothing. Model: `formal/autopilot.tla`, `ApprovedAtStart` and `PersonStopsHold`.

#### Scenario: Unapproved after the pass looked
- **WHEN** a pass saw `add-login` approved, and the owner unapproves it before autopilot's start takes the lock
- **THEN** the start changes nothing and the run stays as it was

#### Scenario: Stopped after the pass looked
- **WHEN** a pass saw `add-login`'s run crashed, and a person resumes and then stops it before autopilot's resume takes the lock
- **THEN** autopilot's resume changes nothing and the run stays stopped

### Requirement: Crashed runs
Autopilot SHALL resume a run whose process died without recording a final state, once per step and round. When the history shows the same step and round already crashed after an autopilot resume, it SHALL leave the run for a person and report it.

#### Scenario: Machine restarted
- **WHEN** the machine restarts while a run is in build, and autopilot is started again
- **THEN** its first pass resumes the run's build step

#### Scenario: Crashes twice
- **WHEN** a run crashes in build, autopilot resumes it, and it crashes again in the same round
- **THEN** no pass resumes it again, and autopilot reports it as needing a person

### Requirement: Asking for a person
When a run on this machine becomes `blocked` or `failed`, or autopilot can't start a change's next group because its approval is stale, autopilot SHALL print one line naming the change and the reason, once per state change, and SHALL carry on with other changes.

#### Scenario: Blocked review
- **WHEN** autopilot runs and `add-login`'s review blocks with `review limit reached`
- **THEN** autopilot prints one line naming `add-login` and the reason, and goes on starting other changes

#### Scenario: Stale before the next group
- **WHEN** a ready run's build edited design.md during group 1
- **THEN** autopilot prints that `add-login` needs approval again before group 2, once

### Requirement: No state of its own
Autopilot SHALL keep no state except its lock and a marker with its process id, machine and start time, which count only while the lock is held. Every decision SHALL come from OpenSpec, run state and history. Runs SHALL keep going when autopilot exits, and a new autopilot SHALL carry on from them.

#### Scenario: Restart mid-run
- **WHEN** autopilot is stopped with Ctrl-C while two runs it started are running, and started again later
- **THEN** the two runs were not stopped, and the new autopilot starts nothing for them while they run

### Requirement: Autopilot settings
`[autopilot]` SHALL accept `scope` (`group` or `change`) and `interval` (a duration of at least 10 seconds). Either config file MAY set them, with the repository's value winning. `tuckerbox check` SHALL reject unknown values and name the file and key.

#### Scenario: Interval too short
- **WHEN** repository config sets `[autopilot] interval = "1s"`
- **THEN** `tuckerbox check` fails and names the file and `autopilot.interval`
