# Spec Delta

## Purpose

Defines how shipping shows in a run's history and on the board: the events a shipping run records, the Shipping column, and the merge request and CI fields on each card.

## ADDED Requirements

### Requirement: Shipping events in history
A shipping run SHALL add these events to its history: `archived` (commit), `pushed` (branch, commit, round), `mr_opened` (link, number), `ci_finished` (commit, `green`, `red` or `none`, failed job names), `review_findings` (count, thread links), `review_replied` (commit, thread links), `merged` (commit, method) and `branch_deleted`. Each event SHALL carry the fields every history event has.

#### Scenario: Failed CI and a fix
- **WHEN** CI fails on the first push and the fix step's push turns it green
- **THEN** the history has `pushed`, `ci_finished` red, `step_started` and `step_finished` for the fix step, `pushed` with round 2, and `ci_finished` green, in that order

#### Scenario: Claim lost
- **WHEN** a run's push is refused because another machine claimed the change
- **THEN** the history has a `blocked` event whose reason names that machine, and no `pushed` event

### Requirement: Shipping column
In a repository with `ship = "mr"` or `"merge"`, the board's columns SHALL include Shipping after the last workflow step's column and before Ready. A run in state `shipping` SHALL be in it, also while a fix step runs. A `blocked` or `failed` run that was shipping SHALL stay in Shipping. With `ship = "local"` there SHALL be no Shipping column.

#### Scenario: Columns in mr mode
- **WHEN** the workflow's steps are build and review, approval is required and `ship = "mr"`
- **THEN** the columns are Proposed, Approved, Queued, Build, Review, Shipping, Ready, Done

#### Scenario: Fix step running
- **WHEN** a shipping run is running its `fix_ci` step
- **THEN** its card is in Shipping with step `fix_ci`

### Requirement: Merged runs
A run in state `merged` SHALL be in Done, with its merge request link, even before the archive reaches the base branch in the local checkout.

#### Scenario: Merged before a fetch
- **WHEN** Tuckerbox merged a change and the local checkout has not fetched since
- **THEN** its card is in Done and links the merged merge request

### Requirement: Shipping on cards
A card whose run has a merge request SHALL include `mr` with its link, number and state (`open`, `merged`, `closed`), and `ci` with its state (`pending`, `green`, `red`, `none`) and the commit it is for. It SHALL include the ship round and `max_rounds`. In the board mod, the card SHALL show the merge request as a link, and CI as a badge.

#### Scenario: Waiting for CI
- **WHEN** a shipping run has pushed round 2 and CI on that commit is running
- **THEN** its card has `mr` with the link, `ci` with state `pending` and that commit, and ship round 2

#### Scenario: Ready in mr mode
- **WHEN** a run is `ready` in `mr` mode with its merge request open and CI green
- **THEN** its card is in Ready, shows the merge request link and a green CI badge, and says it waits for a merge

### Requirement: Board actions while shipping
The board SHALL offer Open merge request on cards with one, opening the link in the person's browser. It SHALL offer Resume on a `ready` card in `mr` mode whose merge request is open, running `tuckerbox resume <change>`. It SHALL NOT offer a merge action.

#### Scenario: Resume from Ready
- **WHEN** the user clicks Resume on a Ready card in `mr` mode with an open merge request
- **THEN** the board runs `tuckerbox resume <change>` and the card moves to Shipping on the next board read
