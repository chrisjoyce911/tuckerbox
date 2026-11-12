# Spec Delta

## Purpose

Defines what Tuckerbox shows about a step's agent while it works: which task it is on, what it has ticked and when, how long it has run, when it last did something, and whether it waits for a person, on board cards, in card detail and in run history.

## ADDED Requirements

### Requirement: The task a run is on
While a step runs, its task SHALL be the first unticked task of the run's scope, or of the whole change without a scope, in the worktree's tasks.md, skipping `(ship)` tasks. When every such task is ticked it SHALL be none, shown as `all tasks in scope ticked`. Tuckerbox SHALL NOT read the task from the agent's output.

#### Scenario: Builder in group 2
- **WHEN** a run with scope `2` is in its build step and tasks 2.1 and 2.2 are ticked in the worktree but 2.3 is not
- **THEN** its task is `2.3` with that task's text

#### Scenario: Everything in scope ticked
- **WHEN** the review step runs after every task of scope `2` was ticked
- **THEN** the card says `all tasks in scope ticked`

### Requirement: Ticks in history
While a step runs, its run process SHALL read the worktree's tasks.md at least every 10 seconds, and append a `task_ticked` or `task_unticked` history event, with the task number, step and round, for each task whose box changed since it last looked.

#### Scenario: Two ticks
- **WHEN** the build agent ticks 2.1, then 2.2 four minutes later
- **THEN** history has `task_ticked` for 2.1 and for 2.2, about four minutes apart, both with step `build`

### Requirement: Times and last activity
For a running step Tuckerbox SHALL know when the step and the run started, and when the agent last did something: the last output written to the step's log in the headless backend, or herdr-core's last output time for its terminal. Times SHALL be stored with their offset and shown in local time.

#### Scenario: Quiet agent
- **WHEN** a build step started 25 minutes ago and its agent last wrote output 12 minutes ago
- **THEN** the card shows the step running for 25 minutes and last activity 12 minutes ago

### Requirement: Waiting for a person
In the terminal backend, a running step's agent state SHALL be `working`, or `waiting for you` when its turn ended without a result or herdr-core reports it waits for input. In the headless backend it SHALL be `working`. A card whose agent waits for you SHALL count as needing the person.

#### Scenario: Permission prompt
- **WHEN** the build agent in a terminal asks to run a command its permission profile doesn't allow
- **THEN** its card shows `waiting for you` and counts in the board's and drawer's needs-you count

### Requirement: Activity in board data
Each card with a running, queued or stopped run SHALL include `activity`: step, agent, model, effort, round, the task (number and text), the last tick (number and time), step and run start times, last activity time, agent state, backend, and for the terminal backend the terminal's address and machine.

#### Scenario: Card with activity
- **WHEN** a dashboard reads `tuckerbox board --json` while `add-login` runs its build step in a terminal on `mini`
- **THEN** its card's `activity` has step `build`, the agent and model, the task, the last tick, the times, agent state, backend `terminal`, and the terminal's address on `mini`

### Requirement: Activity on the board
A running card SHALL show its step, agent and model, its task as `on <n>` with the task's text on hover, the time since the last tick, and the step's running time, and SHALL flag `waiting for you`. Card detail SHALL show every `activity` field, and the ticks of the current run as a timeline, newest first.

#### Scenario: Owner opens a running card
- **WHEN** the owner opens `add-login`'s card while its builder is on task 2.3
- **THEN** the detail shows step `build`, agent `claude` with its model and effort, round, `on 2.3` with its text, 2.2 ticked and when, the running times and the last activity
