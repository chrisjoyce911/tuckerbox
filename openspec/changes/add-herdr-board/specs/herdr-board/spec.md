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
Every board action SHALL run a `tuckerbox` command on the card's machine, and the board SHALL show the command's error if it fails. The board SHALL NOT write run state, and SHALL NOT write any file itself. OpenSpec files change only through the person's editor or a `tuckerbox` command the board runs. Start SHALL be offered only on Approved cards, or on Proposed cards when approval is not required.

#### Scenario: Start from the board
- **WHEN** the user clicks Start on an Approved card
- **THEN** the board runs `tuckerbox run <change>` in the repository and the card moves to Queued or the first step's column when the next board read shows it there

#### Scenario: Start not offered before approval
- **WHEN** approval is required and a card is in Proposed
- **THEN** the card offers Approve but not Start

#### Scenario: Action fails
- **WHEN** Start fails because the base branch has no approval for the change yet
- **THEN** the board shows the command's message and the card stays in Approved

### Requirement: Run actions
The board SHALL offer these actions for the card's state:
- Stop, Resume, and Re-run step
- Follow log: opens a Herdr tab running `tuckerbox logs <change> <step> --follow`
- Open proposal: shows the proposal in a pane beside the board
- Open worktree: opens the change's worktree as a Herdr space

#### Scenario: Follow a running step
- **WHEN** the user clicks Follow log on a card running its build step on machine `mini`
- **THEN** a Herdr tab opens on `mini` running `tuckerbox logs <change> build --follow`

### Requirement: Editing from the board
Card detail SHALL offer Edit for the proposal, design, model, tasks and each spec delta. Edit SHALL open a Herdr pane on the card's machine running `tuckerbox edit <change> <file>`. When that pane closes, the board SHALL run `tuckerbox validate <change> --json` and show the result on the card, kept in memory only. Edit SHALL NOT be offered while the change has a queued or running run.

#### Scenario: Edit leaves the change invalid
- **WHEN** the user edits a spec delta from the board, breaks a requirement and closes the editor
- **THEN** the card shows the validation error

#### Scenario: Edit during a run
- **WHEN** a card is running its build step
- **THEN** its detail offers no Edit, and if a run starts while the edit pane opens, the pane shows the command's refusal

### Requirement: Tasks and approval from the board
Card detail SHALL let the person tick and untick each task by running `tuckerbox tasks <change> tick|untick <n.m>`, except while the change has a queued or running run. It SHALL show Approve when the approval state is `none` or `stale`, and Unapprove when it is `approved`, running `tuckerbox approve` or `tuckerbox unapprove`. It SHALL show the command's note that nothing was committed.

#### Scenario: Tick from the board
- **WHEN** the user ticks task 2.1 on a stopped card
- **THEN** the board runs `tuckerbox tasks <change> tick 2.1` and the next board read shows 2.1 ticked

#### Scenario: Approve from the board
- **WHEN** the user clicks Approve on a valid Proposed card
- **THEN** the board runs `tuckerbox approve <change>`, shows that the approval is not committed, and the card moves to Approved on the next board read

#### Scenario: Approve an invalid change
- **WHEN** the user clicks Approve and the change fails strict validation
- **THEN** the board shows the validation errors and the card stays in Proposed

### Requirement: Card detail view
Opening a card SHALL show:
- the proposal, rendered as markdown
- the task list with progress and tick boxes
- the approval state, who approved and when, and whether it is committed
- the last validation result from an edit, if any
- dependencies with their columns
- each round with its result and findings
- the history, newest first, times in local time
- the actions available for the card's state

#### Scenario: Rejected change
- **WHEN** the user opens a card whose review rejected round 1
- **THEN** the detail shows round 1's findings under round 1, and the history shows the hand-off back to the build step
