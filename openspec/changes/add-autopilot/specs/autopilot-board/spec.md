# Spec Delta

## Purpose

Defines how autopilot shows on the Tuckerbox board: whether it is running, which cards need a person, and the Herdr toast when one starts needing a person.

## ADDED Requirements

### Requirement: Autopilot in the board document
The board document SHALL include `autopilot`: whether it is running for the repository on this machine, and if so its process id and start time, read from its lock and marker. A marker without the lock held SHALL count as not running.

#### Scenario: Autopilot running
- **WHEN** autopilot runs for the repository and a dashboard reads `tuckerbox board --json`
- **THEN** the document has `autopilot` with running `true`, its process id and start time

#### Scenario: Leftover marker
- **WHEN** autopilot was killed and left its marker, and nothing holds its lock
- **THEN** the document has `autopilot` with running `false`

### Requirement: Cards that need a person
A card SHALL have `needs_you` with a reason when its run is `blocked` or `failed`, when it has a ready run with unticked tasks and its approval is `stale`, or when `[autopilot] scope = "change"` and it has a ready run without a scope whose change is not complete. The board header's "needs you" count SHALL count these cards.

#### Scenario: Whole change left ready
- **WHEN** `[autopilot] scope = "change"` and a ready run without a scope left task 3.2 unticked
- **THEN** its card has `needs_you` with reason `tasks not ticked: 3.2; run it again by hand`

#### Scenario: Next group waits for approval
- **WHEN** a change's run is ready with scope `1`, group 2 is unticked, and its approval is stale
- **THEN** its card has `needs_you` with reason `approve again to run the next group`

### Requirement: Toast when a card needs you
The board mod SHALL show a Herdr toast naming the change and reason when a card starts needing a person between two board reads. It SHALL NOT toast for cards that already needed a person at its first read.

#### Scenario: Run blocks
- **WHEN** the board or drawer is open and a run becomes blocked
- **THEN** one toast names the change and the reason

#### Scenario: Board opened later
- **WHEN** the board opens while two cards already need a person
- **THEN** it shows no toast, and the header counts 2 needing you

### Requirement: Autopilot in the header
The board header SHALL show whether autopilot is on for the repository on the board's machine.

#### Scenario: Autopilot off
- **WHEN** no autopilot runs for the repository
- **THEN** the header shows autopilot as off
