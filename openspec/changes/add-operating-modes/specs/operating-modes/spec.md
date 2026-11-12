# Spec Delta

## Purpose

Defines how a repository chooses the way its changes run: on one machine (solo), on several of one owner's machines (fleet), or by several people (team), and what each mode requires.

## ADDED Requirements

### Requirement: Mode setting
Repository config `mode` SHALL accept `solo`, `fleet` and `team`, and default to `solo`. It SHALL be read only from repository config, so every machine runs the repository the same way. `tuckerbox check` SHALL fail when machine config sets it, or on an unknown value, and name the key.

#### Scenario: Default mode
- **WHEN** a repository's config does not set `mode`
- **THEN** it runs in solo mode

#### Scenario: Machine config sets the mode
- **WHEN** machine config contains `mode = "fleet"`
- **THEN** `tuckerbox check` exits non-zero and says `mode` belongs in repository config

### Requirement: Solo mode
In solo mode, runs SHALL behave as before this change: no claim is written to the remote, `machines` preferences are ignored, and `tuckerbox dispatch` SHALL refuse and say the repository is solo.

#### Scenario: Dispatch in a solo repository
- **WHEN** the user runs `tuckerbox dispatch` in a repository without `mode`
- **THEN** it exits non-zero, says the repository is solo, and starts nothing

### Requirement: Fleet mode needs a remote
In fleet mode, `tuckerbox check` SHALL fail when the repository has no remote to hold the fleet ref, and `tuckerbox run` SHALL refuse to start. The remote SHALL be `[shipping] remote` if set, else `origin`.

#### Scenario: Fleet without a remote
- **WHEN** `mode = "fleet"` and the repository has no `origin` remote
- **THEN** `tuckerbox check` exits non-zero and says fleet mode needs a remote

### Requirement: Team mode needs forge shipping
In team mode, `tuckerbox check` SHALL fail unless `ship` is `mr` or `merge`, because the claim is a merge request on the forge. `tuckerbox run` SHALL refuse to start when no forge token is found, as a shipping run does.

#### Scenario: Team with local shipping
- **WHEN** `mode = "team"` and `ship = "local"`
- **THEN** `tuckerbox check` exits non-zero and says team mode needs `ship = "mr"` or `"merge"`

### Requirement: Changing mode
Changing `mode` SHALL NOT change any run already started. A fleet claim left on the remote after a move to solo or team SHALL stay until its run ends or `tuckerbox claim release` removes it, and `tuckerbox check` SHALL list such leftover claims.

#### Scenario: Fleet to solo with a claim left
- **WHEN** a repository moves from fleet to solo while change `add-login` holds a fleet claim
- **THEN** `tuckerbox check` passes and lists the leftover claim on `add-login` with its machine
