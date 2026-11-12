# Spec Delta

## Purpose

Defines how a person edits a change's OpenSpec files and ticks its tasks through Tuckerbox, and how those edits are kept out of the way of a run.

## ADDED Requirements

### Requirement: Where edits happen
`tuckerbox edit`, `tuckerbox tasks`, `tuckerbox approve` and `tuckerbox unapprove` SHALL act on the change's files in its worktree when the change has one on this machine, and otherwise in the repository checkout the command runs in. Each SHALL print the folder it acted in. None of them SHALL commit, and each SHALL say that its change is not committed.

#### Scenario: Change with a worktree
- **WHEN** change `add-login` has a worktree on this machine and the user runs `tuckerbox edit add-login design`
- **THEN** the editor opens `design.md` in that worktree, not in the repository checkout

#### Scenario: Change without a worktree
- **WHEN** change `add-login` has never run on this machine
- **THEN** `tuckerbox edit add-login design` opens `design.md` in the repository checkout

### Requirement: Editing a change
`tuckerbox edit <change> <file>` SHALL open one of the change's files in the person's editor. `<file>` is `proposal`, `design`, `model`, `tasks` or `spec:<capability>`. The editor is `editor` from machine config, else `$VISUAL`, else `$EDITOR`. A repository SHALL NOT set the editor. When the editor exits, the command SHALL validate the change as `tuckerbox validate` does, print the result and exit with its status.

#### Scenario: No editor set
- **WHEN** machine config has no `editor` and neither `$VISUAL` nor `$EDITOR` is set
- **THEN** `tuckerbox edit` exits non-zero, opens nothing and says how to set an editor

#### Scenario: Edit breaks a spec
- **WHEN** the user removes the only scenario from a requirement and closes the editor
- **THEN** `tuckerbox edit` prints the strict validation error for that requirement and exits non-zero

#### Scenario: Unknown spec
- **WHEN** the user runs `tuckerbox edit add-login spec:billing` and the change has no `billing` spec delta
- **THEN** it exits non-zero, opens nothing and lists the change's spec deltas

### Requirement: Validating a change
`tuckerbox validate <change> [--json]` SHALL run `openspec validate <change> --strict` in the folder where edits happen and report a pass or every error. `--json` SHALL print the result as a JSON document with a `version` field, under the same rules as `tuckerbox status --json`. It SHALL exit 0 only when the change is valid.

#### Scenario: Valid change
- **WHEN** a dashboard runs `tuckerbox validate add-login --json` on a valid change
- **THEN** it receives a document with `version`, `valid: true` and no errors, and exit status 0

### Requirement: Ticking tasks
`tuckerbox tasks <change> tick <n.m>` and `tuckerbox tasks <change> untick <n.m>` SHALL change only the checkbox of task `n.m` in tasks.md and leave every other byte as it was. An unknown task number SHALL be refused. Ticking a ticked task, or unticking an unticked one, SHALL change nothing and exit 0.

#### Scenario: Tick a task
- **WHEN** the user runs `tuckerbox tasks add-login tick 2.1`
- **THEN** task 2.1's line reads `- [x] 2.1 …` and the rest of tasks.md, line endings included, is unchanged

#### Scenario: Unknown task
- **WHEN** the user runs `tuckerbox tasks add-login tick 9.9` and tasks.md has no task 9.9
- **THEN** it exits non-zero, changes nothing and says no task 9.9 exists

### Requirement: No edits during a run
`tuckerbox edit`, `tuckerbox tasks`, `approve` and `unapprove` SHALL refuse while the change has an active (queued or running) run on this machine, and name its state. They SHALL hold the change's run lock while they write, and `edit` while the editor is open, so no run can start meanwhile. A run or resume that finds an edit holding the lock SHALL refuse and say so. Model: `formal/runs.tla`, `OneRunPerChange`.

#### Scenario: Tick during a build step
- **WHEN** change `add-login` is running its build step and the user runs `tuckerbox tasks add-login tick 1.1`
- **THEN** it exits non-zero, tasks.md is unchanged and the message names the running build step

#### Scenario: Resume while the editor is open
- **WHEN** `tuckerbox edit add-login design` has the editor open on a stopped run and the user runs `tuckerbox resume add-login`
- **THEN** resume refuses and says the change is being edited

#### Scenario: Edit a blocked run
- **WHEN** a run is `blocked` because its review limit was reached
- **THEN** `tuckerbox edit` opens the editor, because no step is running
