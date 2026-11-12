# Spec Delta

## Purpose

Defines how one OpenSpec change is taken through a repository's workflow on one machine, in its own worktree, and how several such runs share the machine.

## ADDED Requirements

### Requirement: Selecting changes
`tuckerbox run` SHALL accept one or more OpenSpec change ids, or `--all` for every active change that `openspec` reports as valid and not yet archived. It SHALL refuse a change that fails `openspec validate`, and report why.

#### Scenario: Invalid change
- **WHEN** the user runs `tuckerbox run fix-login` and `openspec validate fix-login` fails
- **THEN** no worktree or run is created and the validation errors are shown

### Requirement: One worktree and branch per change
Each run SHALL work in its own git worktree on its own branch, created from the repository's configured base branch. By default the base is the remote default branch after a fetch, or the local default branch if there is no remote. The worktree location and branch name pattern SHALL be configurable. Re-running a change SHALL reuse its existing worktree and branch and never discard uncommitted work in it.

#### Scenario: First run of a change
- **WHEN** a change has no worktree yet
- **THEN** Tuckerbox creates the branch and worktree and starts the first step there

#### Scenario: Existing worktree with uncommitted work
- **WHEN** the change's worktree already exists with uncommitted changes
- **THEN** Tuckerbox resumes in it without resetting, cleaning or stashing anything

### Requirement: Steps run in workflow order
A run SHALL execute the workflow's steps in order. A step finishes when its agent reports a result or its process exits. A step's result is one of `done`, `rejected`, `blocked` or `failed`.
- `done` starts the next step.
- `rejected` goes to the step named by `on_reject`.
- `blocked` pauses the run until a person resumes it.
- `failed` stops the run.

#### Scenario: Reviewer rejects
- **WHEN** the review step reports `rejected` with findings and `on_reject = "build"`
- **THEN** the build step starts again and its brief includes the review's findings

#### Scenario: Agent exits without a result
- **WHEN** a step's agent process exits without writing a valid result
- **THEN** the step is recorded as `failed` with the process's exit status and the tail of its output

### Requirement: Bounded review loops
Each `on_reject` loop SHALL have a maximum round count, 3 by default. When the limit is reached, the run SHALL pause as `blocked` rather than loop again.

#### Scenario: Limit reached
- **WHEN** the review step rejects for the third time with `max_rounds = 3`
- **THEN** the run is marked `blocked` with reason `review limit reached` and no further step starts

### Requirement: Local ship mode
With `ship = "local"`, a run that finishes its last step SHALL leave the work committed on the change's branch, record the run as `ready`, and push nothing.

#### Scenario: Run completes locally
- **WHEN** the last step reports `done` in local mode
- **THEN** the branch holds the commits, the worktree is kept, and the run's state is `ready`

### Requirement: Parallel limit
Tuckerbox SHALL run at most `max_parallel` changes at once per repository on one machine. It SHALL queue further changes and start them in the order requested as slots free up. At most one run per change SHALL be active on a machine at any time. This holds even when several `tuckerbox` processes start runs at once. Model: `formal/runs.tla`, invariants `OneRunPerChange` and `WithinLimit`.

#### Scenario: More changes than slots
- **WHEN** `max_parallel = 2` and the user runs `tuckerbox run --all` with four valid changes
- **THEN** two runs start, two are `queued`, and each queued run starts when a running one ends or pauses

#### Scenario: Change already running
- **WHEN** a change already has an active run and the user starts it again
- **THEN** Tuckerbox refuses and points to the active run

### Requirement: Stop, resume and re-run
`tuckerbox stop <change>` SHALL end the active step's agent and mark the run `stopped`. `tuckerbox resume <change>` SHALL continue a `stopped` or `blocked` run from its current step. `tuckerbox rerun <change> <step>` SHALL start again from the named step. None of these SHALL alter files in the worktree.

#### Scenario: Resume after a stop
- **WHEN** a run was stopped during its review step and the user runs `tuckerbox resume`
- **THEN** the review step starts again in the same worktree
