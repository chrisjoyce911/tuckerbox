# Spec Delta

## Purpose

Defines tasks only the owner can do (checks by hand), so autopilot never starts an agent that can only wait for a person.

## ADDED Requirements

### Requirement: Owner task marker
A task whose line starts with `(owner)` after its number SHALL be one only a person can do. A builder SHALL NOT tick it. `tuckerbox` SHALL list a change's open owner tasks in `status`, `status --json` and on the card as `needs you: <task numbers>`.

#### Scenario: Owner task listed
- **WHEN** `fix-board-safety` has open tasks `2.19 (owner) …` and `2.20 (owner) …`
- **THEN** its card and `tuckerbox status` show `needs you: 2.19, 2.20`

### Requirement: No run for owner tasks only
Autopilot and `tuckerbox run --tasks next` SHALL NOT start a step when every open task in the next group, other than `(ci)` and `(ship)` tasks, is an `(owner)` task. The run SHALL be `blocked` with reason `owner tasks: <numbers>` (needs you) without starting an agent. When the owner ticks them, the run SHALL continue on the next pass.

#### Scenario: Only hand checks left
- **WHEN** autopilot passes over `fix-board-safety` and its only open tasks are owner tasks 2.19–2.22 and `(ci)` task 3.1
- **THEN** no agent starts and the run is `blocked` with reason `owner tasks: 2.19, 2.20, 2.21, 2.22`

#### Scenario: Owner ticks the checks
- **WHEN** the owner ticks 2.19–2.22 on the default branch
- **THEN** the next autopilot pass ships the change

### Requirement: No restart of an unchanged block
Autopilot SHALL NOT restart a run that ended `blocked` or `failed` when nothing changed since it ended: no new commit on its branch or the default branch touching the change, no task edit, no answer and no approval change. It SHALL leave the run as it is and show the earlier reason.

#### Scenario: Same block twice
- **WHEN** a build step blocked with `waiting for the owner's hand checks` and nothing changed
- **THEN** the next autopilot pass does not start the run again
