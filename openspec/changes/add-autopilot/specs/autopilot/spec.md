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

#### Scenario: Whole change left incomplete
- **WHEN** `scope = "change"`, `ship = "merge"`, and a run's last step finishes with task 3.2 unticked
- **THEN** the run is `blocked` (task-scope, `Incomplete whole-change runs`), autopilot reports it once, and no pass restarts it

### Requirement: Shipping is the run's
Autopilot SHALL NOT archive, push or merge itself. In `mr` and `merge` mode, a run ships when its change is complete and stays `ready` otherwise, as task-scope and add-forge-shipping say. A change SHALL count as finished for autopilot once it is archived on the base branch.

#### Scenario: First group in merge mode
- **WHEN** `ship = "merge"` and a run with scope `1` finishes while group 2 is unticked
- **THEN** the run is `ready`, nothing is archived or pushed, and the next pass starts group 2

#### Scenario: Last group in merge mode
- **WHEN** `ship = "merge"`, the last group's run finishes with every task ticked except `(ship)` tasks, and the merge request merges
- **THEN** the change is archived on the base branch, and later passes start nothing for it

### Requirement: Autopilot never overrides a person
Autopilot SHALL NOT approve, unapprove, edit or tick, SHALL NOT resume a `blocked` run or one a person stopped, and SHALL NOT re-run a `failed` one. It SHALL leave a change alone while a person's edit holds its lock.

#### Scenario: Person stopped a run
- **WHEN** a person ran `tuckerbox stop add-login`
- **THEN** no pass resumes or restarts `add-login`

#### Scenario: Blocked run
- **WHEN** a run is `blocked` with reason `review limit reached`
- **THEN** no pass resumes it

### Requirement: Acting on what it saw
Each start or resume autopilot makes SHALL carry the state and `since` time it read in the run state. Under the change lock, the engine SHALL go ahead only if both still match and the approval rule holds: a start needs the change `approved`; a crash resume needs it `approved` or `stale`, not `none`. Otherwise it SHALL change nothing. Model: `formal/autopilot.tla`.

#### Scenario: Unapproved after the pass looked
- **WHEN** a pass saw `add-login` approved, and the owner unapproves it before autopilot's start takes the lock
- **THEN** the start changes nothing and the run stays as it was

#### Scenario: Stopped after the pass looked
- **WHEN** a pass saw `add-login`'s run crashed, and a person resumes and then stops it before autopilot's resume takes the lock
- **THEN** autopilot's resume changes nothing and the run stays stopped

#### Scenario: Crashed again after a person resumed
- **WHEN** a pass saw `add-login`'s run crashed, and a person resumes it and it crashes again before autopilot's resume takes the lock
- **THEN** autopilot's resume changes nothing, because `since` changed, and the next pass judges the new crash

#### Scenario: Unapproved while crashed
- **WHEN** a run crashed and the owner then ran `tuckerbox unapprove add-login`
- **THEN** no pass resumes it

#### Scenario: Stale approval from the run's own step
- **WHEN** a run's build step edited design.md, making the approval `stale`, and the run then crashed
- **THEN** the next pass resumes it, as `tuckerbox resume` would

### Requirement: Crashed runs
Autopilot SHALL resume a run whose process died without recording a final state only while the run's history shows no autopilot resume since the run's last step result, or its last start, resume or rerun by a person. Rounds SHALL NOT reset this count: each resume still starts a new round. Otherwise it SHALL leave the run and report it.

#### Scenario: Machine restarted
- **WHEN** the machine restarts while a run is in build, and autopilot is started again
- **THEN** its first pass resumes the run's build step, in a new round

#### Scenario: Crashes twice across rounds
- **WHEN** a run crashes in build round 1, autopilot resumes it as round 2, and it crashes again before its step reports a result
- **THEN** no pass resumes it again, also after autopilot restarts, and autopilot reports it as needing a person

#### Scenario: Progress renews the budget
- **WHEN** autopilot resumed a crashed build, the build then reported `done`, and the review step later crashes
- **THEN** the next pass resumes the review step

#### Scenario: A person renews the budget
- **WHEN** autopilot left a run after its second crash, and a person runs `tuckerbox resume`
- **THEN** a later crash of that run is resumed once again by autopilot

### Requirement: Asking for a person
When a run on this machine becomes `blocked` or `failed`, autopilot leaves a crashed run, or it can't start a change's next group because its approval is stale, autopilot SHALL print one line naming the change and the reason, once per state change, and SHALL carry on with other changes.

#### Scenario: Blocked review
- **WHEN** autopilot runs and `add-login`'s review blocks with `review limit reached`
- **THEN** autopilot prints one line naming `add-login` and the reason, and goes on starting other changes

#### Scenario: Stale before the next group
- **WHEN** a ready run's build edited design.md during group 1
- **THEN** autopilot prints that `add-login` needs approval again before group 2, once

### Requirement: Operating modes
In `solo` mode, autopilot SHALL start runs on this machine as this capability says. In `fleet` mode, each pass SHALL run `tuckerbox dispatch` (add-operating-modes) for placement instead of starting runs itself, and SHALL still report runs that need a person. In `team` mode, autopilot SHALL refuse to start.

#### Scenario: Fleet mode
- **WHEN** the repository sets `mode = "fleet"` and autopilot makes a pass
- **THEN** it runs `tuckerbox dispatch` and starts no run on this machine itself

#### Scenario: Team mode
- **WHEN** the repository sets `mode = "team"` and the user runs `tuckerbox autopilot`
- **THEN** it exits non-zero and says autopilot is for solo and fleet repositories

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
