# change-dependencies Specification

## Purpose
Defines how an OpenSpec change declares the other changes it depends on, and how Tuckerbox holds a run until they are ready.

## Requirements

### Requirement: Declaring dependencies
A change MAY list the changes it depends on in `tuckerbox.toml` inside its change folder, as `depends_on = ["<change-id>", …]`. The file travels with the change and is archived with it. `tuckerbox check` SHALL report a dependency that names neither an active nor an archived change, and any dependency cycle.

#### Scenario: Unknown dependency
- **WHEN** a change depends on `add-login` and no active or archived change has that id
- **THEN** `tuckerbox check` fails and names both changes

#### Scenario: Cycle
- **WHEN** change A depends on B and B depends on A
- **THEN** `tuckerbox check` fails and lists the cycle

### Requirement: Waiting for dependencies
A run SHALL NOT start its first step until every dependency is `ready` on this machine or archived. While waiting, its state SHALL be `queued`, with the unmet dependencies as its reason. It SHALL NOT hold a parallel slot while waiting.

#### Scenario: Dependency still building
- **WHEN** the user starts change B, which depends on A, and A is in its build step
- **THEN** B is `queued` with reason `waiting on A (build)`, and starts once A becomes `ready`

#### Scenario: Dependency archived
- **WHEN** change B depends on A and A has been archived
- **THEN** B can start without waiting

#### Scenario: Waiting holds no slot
- **WHEN** `max_parallel` is 1, change B waits on A, which has not started, and the user starts change C
- **THEN** C runs while B stays `queued` with reason `waiting on A (not started)`

### Requirement: Checking dependencies again at admission
A run SHALL check its dependencies again once it has taken a parallel slot. If one is no longer met, for example because it was re-run, the run SHALL give the slot back and wait for its dependencies again.

#### Scenario: Dependency re-run while waiting for a slot
- **WHEN** change B waits for a slot after its dependency A became `ready`, and A is re-run and queued before B gets the slot
- **THEN** B gives the slot back once it gets it, stays `queued` with reason `waiting on A (queued)`, and starts after A is `ready` again

### Requirement: Unknown dependency at run time
A run whose dependency names neither an active nor an archived change SHALL fail, naming it, rather than wait. Whether a change exists SHALL be decided from the change folders alone: a run record left behind by a change removed without archiving does not make it known.

#### Scenario: Unknown dependency at run time
- **WHEN** the user starts change B, which depends on `add-login`, and no active or archived change has that id
- **THEN** the run fails with a reason naming `add-login`, and no step starts

#### Scenario: Removed change with a run record
- **WHEN** change B depends on A, A was removed without archiving, and this machine still has A's stopped run
- **THEN** the run of B fails with a reason naming A
