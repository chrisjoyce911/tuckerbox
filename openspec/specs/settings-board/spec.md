# settings-board Specification

## Purpose
Defines how a change's effective step settings show on the board, and how a person changes them from card detail through `tuckerbox set`.

## Requirements

### Requirement: Effective settings on cards
Each active card SHALL include `settings`: one entry per workflow step, in workflow order, with the effective `agent`, `model`, `effort` and `permissions`, each with its source (`default`, `machine`, `repo` or `change`). It SHALL include the change's `machines` list. A run's card SHALL show the settings its current step started with.

#### Scenario: Card with an override
- **WHEN** change `add-login` overrides only `[steps.build] agent = "codex"`
- **THEN** its card's build entry has agent `codex` from `change`, and the other entries' values come from `repo` or `default`

#### Scenario: Running step started before an edit
- **WHEN** the build step started with model `opus`, and the model was then changed in the repository config
- **THEN** the card's build entry shows `opus`, the value the step started with

### Requirement: Allowed choices in board data
`tuckerbox board --json --change <id>` SHALL include `choices`: the repository's allow-list (agents, models per agent, efforts and profile names), and which keys void approval. With no `[overrides]`, `choices` SHALL be empty.

#### Scenario: Choices
- **WHEN** `[overrides]` allows agents `claude` and `codex` and efforts `medium` and `high`
- **THEN** card detail's `choices` lists both agents and both efforts, and marks `agent` and `permissions` as voiding approval

### Requirement: Settings pickers
Card detail SHALL show the effective settings per step with their source. It SHALL offer a picker for each key the allow-list permits, holding only allowed values and a "repository default" entry. Choosing a value SHALL run `tuckerbox set <change> step.<step>.<key>=<value>`; the default entry SHALL run `--unset`. Pickers SHALL NOT be offered while a run is queued or running.

#### Scenario: Pick a model
- **WHEN** the user picks model `sonnet` for the review step on a stopped card
- **THEN** the board runs `tuckerbox set <change> step.review.model=sonnet`, shows that nothing was committed, and the next board read shows `sonnet` from `change`

#### Scenario: No pickers while running
- **WHEN** a card is running its build step
- **THEN** its settings show without pickers

### Requirement: Warning before voiding approval
When the change is approved, a picker for `agent` or `permissions` SHALL say, before running the command, that the choice will void approval, and run it only if the person confirms. Pickers for `model`, `effort` and `machines` SHALL NOT ask.

#### Scenario: Change agent on an approved card
- **WHEN** the user picks agent `codex` for the build step of an Approved card
- **THEN** the board says approval will be voided, and on confirmation runs `tuckerbox set` and the card moves to Proposed with approval `stale`
