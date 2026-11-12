# Spec Delta

## Purpose

Defines how a run ends when its change was shipped some other way, and how a person clears a run, so no run stays blocked for a change that is gone.

## ADDED Requirements

### Requirement: Runs of changes merged elsewhere
When a run has no agent running (`queued`, `blocked`, `stopped`, `failed`, `ready` or crashed) and its change is archived on the fetched default branch, or its merge request was merged, Tuckerbox SHALL mark it `merged` under the change's run lock, with reason `archived on <branch>` or `merge request merged`, and record it in history. A running run SHALL be closed once it has no agent. Model: `formal/autopilot.tla`, `ClosesOnlyIdleArchived`.

#### Scenario: Archived on main by hand
- **WHEN** `add-login`'s run is `blocked` and someone archives `add-login` on `main`
- **THEN** the run is marked `merged` with reason `archived on main`

#### Scenario: Merged by hand while blocked
- **WHEN** `add-login`'s run is `blocked` with reason `ship limit reached` and a person merges its pull request
- **THEN** the run becomes `merged` with reason `merge request merged`

#### Scenario: Agent still running
- **WHEN** `add-login` is archived on `main` while its review step's agent runs
- **THEN** the run is not closed until the step ends; then it is marked `merged`

#### Scenario: Folder deleted without an archive
- **WHEN** `add-login`'s folder is gone from `main` and no archive entry exists
- **THEN** the run is left as it is and `tuckerbox status` notes that the change is missing on `main`

### Requirement: Archived on the default branch
A change SHALL count as archived on the default branch only when its folder is gone from `openspec/changes/` there and `openspec/changes/archive/<date>-<change>/` exists.

#### Scenario: Archive entry present
- **WHEN** `main` has `openspec/changes/archive/2026-11-12-add-login/` and no `openspec/changes/add-login/`
- **THEN** `add-login` counts as archived on `main`

### Requirement: When merged runs are noticed
Autopilot SHALL check for runs of changes merged elsewhere on every pass; `tuckerbox status` and `board --json` SHALL check at most once a minute, after fetching the default branch.

#### Scenario: Status notices
- **WHEN** autopilot isn't running and `add-login` was archived on `main` with its run `blocked`
- **THEN** `tuckerbox status` fetches, marks the run `merged`, and does not list it

### Requirement: Closing a run by hand
`tuckerbox close <change> [--reason <text>]` SHALL mark a run with no agent running `closed`, under the change's run lock, with the reason (default `closed by <user>`), record it in history and give up any queue place. It SHALL refuse a running run, saying to stop it first, and SHALL exit non-zero when the change has no run. It SHALL NOT change the worktree, the branch or the merge request. The board SHALL offer Close on a card whose run has no agent running.

#### Scenario: Clear a blocked run
- **WHEN** `add-login`'s run is `blocked` and the user runs `tuckerbox close add-login`
- **THEN** the run is `closed` with reason `closed by` the user, and the worktree is kept

#### Scenario: Close while running
- **WHEN** `add-login`'s build step is running and the user runs `tuckerbox close add-login`
- **THEN** it refuses and says to run `tuckerbox stop add-login` first

### Requirement: Closed runs stay closed
Autopilot SHALL NOT start, resume or close again a `merged` or `closed` run. `tuckerbox run <change>` on a change that is still active SHALL start a new run.

#### Scenario: Autopilot after a close
- **WHEN** the user closed `add-login`'s run and `add-login` is still approved
- **THEN** autopilot does not start it; `tuckerbox run add-login` does

### Requirement: Closed runs in status and on the board
`tuckerbox status` and `status --json` SHALL list `merged` and `closed` runs only with `--all`, with their reason. The board SHALL show no run on a card whose run is `merged` or `closed`.

#### Scenario: Default listing
- **WHEN** one run is `blocked` and another is `closed`
- **THEN** `tuckerbox status` lists only the blocked run, and `tuckerbox status --all` lists both
