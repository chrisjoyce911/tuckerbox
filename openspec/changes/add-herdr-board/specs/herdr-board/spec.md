# Spec Delta

## Purpose

Defines the Tuckerbox board inside the Herdr app: what it shows, what its actions do, and how it brings several machines together.

## ADDED Requirements

### Requirement: Installing the mod
`tuckerbox herdr install` SHALL copy the board mod into the Herdr app's mods folder, run `herdr-app check` on it, and report the result. If Herdr is not installed, it SHALL say so and change nothing. Re-running it SHALL update the mod in place.

#### Scenario: Herdr missing
- **WHEN** a machine has no Herdr app config folder
- **THEN** `tuckerbox herdr install` exits non-zero, says Herdr wasn't found and writes nothing

### Requirement: Board for a space's repository
The board SHALL show the repository of the Herdr space it is opened from, using `tuckerbox board --json`. It SHALL refresh when run state changes, and at most every 5 seconds while shown. The header SHALL show the repository, the count of running changes, and the count of changes needing attention.

#### Scenario: Space outside a repository
- **WHEN** the board is opened from a space whose folder is not a Tuckerbox repository
- **THEN** it says so and offers to run `tuckerbox init` in that folder

### Requirement: Runs from every machine
The board SHALL ask each connected Herdr machine for the same repository's board, matched by remote URL, and merge the cards by change id. Each card SHALL show the machine its run is on. An unreachable machine SHALL be named in the header and SHALL NOT hide the other machines' cards.

#### Scenario: Two machines
- **WHEN** change A runs on machine `studio` and change B on machine `mini`, both in the same repository
- **THEN** one board shows both cards, each labelled with its machine

### Requirement: Actions go through Tuckerbox
Every board action SHALL run a `tuckerbox` command on the card's machine, and the board SHALL show the command's error if it fails. The board SHALL NOT write run state or OpenSpec files. The actions are:
- Start a Proposed card
- Stop, Resume, and Re-run step
- Follow log: opens a Herdr tab running `tuckerbox logs <change> <step> --follow`
- Open proposal: opens the proposal in a pane beside the board
- Open worktree: opens the change's worktree as a Herdr space

#### Scenario: Start from the board
- **WHEN** the user clicks Start on a Proposed card
- **THEN** the board runs `tuckerbox run <change>` in the repository and the card moves to Queued or the first step's column when the next board read shows it there

#### Scenario: Action fails
- **WHEN** Start fails because the change doesn't validate
- **THEN** the board shows the validation error and the card stays in Proposed

### Requirement: Card detail view
Opening a card SHALL show:
- the proposal, rendered as markdown
- the task list with progress
- dependencies with their columns
- each round with its result and findings
- the history, newest first, times in local time
- the actions available for the card's state

#### Scenario: Rejected change
- **WHEN** the user opens a card whose review rejected round 1
- **THEN** the detail shows round 1's findings under round 1, and the history shows the hand-off back to the build step
