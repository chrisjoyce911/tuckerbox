# Spec Delta

## Purpose

Defines how operating modes show on the board: the repository's mode, each change's claim, the machine and person holding it, and the Dispatch and Take over actions.

## ADDED Requirements

### Requirement: Mode in the board document
`tuckerbox board --json` SHALL include the repository's `mode`. In fleet mode it SHALL include `fleet` with `max_runs` and the number of claims counted against it.

#### Scenario: Fleet board
- **WHEN** a fleet repository has `max_runs = 3` and two claimed changes
- **THEN** the document has mode `fleet` and fleet `max_runs` 3 with 2 claims

### Requirement: Claims on cards
In fleet and team mode, each card with a claim SHALL include `claim`: the machine, the state (`active`, `parked`, `expired` or `ready` in fleet; `open` in team), and in fleet mode the epoch and last renewal. In team mode it SHALL include the owner's forge login. A claimed card with no run on any connected machine SHALL still show its claim.

#### Scenario: Expired claim on a card
- **WHEN** `add-login`'s fleet claim on `mini` expired and `mini` is not connected
- **THEN** its card shows claim state `expired`, machine `mini` and the last renewal time

#### Scenario: Owner in a team
- **WHEN** login `sam` holds `add-login` from machine `studio`
- **THEN** its card shows owner `sam` on `studio`

### Requirement: Dispatch from the board
In fleet mode, the board header SHALL offer Dispatch, which runs `tuckerbox dispatch` on this machine and shows its output. It SHALL offer it only when at least one Approved card has no claim.

#### Scenario: Dispatch from the board
- **WHEN** the user clicks Dispatch with two unclaimed Approved cards
- **THEN** the board runs `tuckerbox dispatch` and shows where each change was placed or why not

### Requirement: Take over from the board
In fleet mode, a card whose claim is expired SHALL offer Take over, which runs `tuckerbox run --take-over <change>` on this machine after the person confirms that work on the old machine is left behind.

#### Scenario: Take over an expired card
- **WHEN** the user clicks Take over on an expired card and confirms
- **THEN** the board runs `tuckerbox run --take-over <change>` and the card shows this machine as the holder on the next read
