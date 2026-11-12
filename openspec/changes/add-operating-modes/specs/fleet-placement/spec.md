# Spec Delta

## Purpose

Defines how `tuckerbox dispatch` chooses which approved changes to start in a fleet repository and on which machines, and how it starts them there, in one pass and without a daemon.

## ADDED Requirements

### Requirement: One dispatch pass
`tuckerbox dispatch [<change>...]` SHALL, in a fleet repository, fetch, pick the changes to start, place each on a machine, start them, print what it did, and exit. It SHALL NOT keep running or install a schedule. With change ids, it SHALL consider only those changes.

#### Scenario: Nothing to do
- **WHEN** every approved change is claimed or waiting on a dependency
- **THEN** `tuckerbox dispatch` starts nothing, says why for each change, and exits 0

### Requirement: Changes to start
Dispatch SHALL consider only active changes that are valid, pass the run gate, have their dependencies met, and have no claim in the fleet ref. It SHALL take them in the order of the board's Approved column, and stop once the fleet limit is reached.

#### Scenario: Unapproved change skipped
- **WHEN** change `add-login` is valid but its approval is stale
- **THEN** dispatch does not place it and lists it as `not approved`

### Requirement: Continuing ready runs
Dispatch SHALL also continue each approved change whose fleet claim is `ready` and whose run is not complete, on the machine holding the claim, as autopilot's `Incomplete ready runs` says: with `--tasks next` when the run has a scope or the repository's `[autopilot] scope` is `group`; otherwise it SHALL report it and not continue it. Complete ready runs and parked claims SHALL NOT be continued.

#### Scenario: Next group across passes
- **WHEN** `max_runs = 1`, `add-login` is ready on `studio` after group 1 with group 2 unticked, and `add-users` holds the only room on `mini`
- **THEN** a pass reports `add-login` waiting for room and starts nothing; once `add-users` is ready, the next pass runs `tuckerbox run --for <remote> --tasks next add-login` on `studio`, which reactivates the claim

#### Scenario: Complete ready run
- **WHEN** `add-login` is ready on `studio` with every task ticked, waiting for its merge request
- **THEN** dispatch does not continue it

#### Scenario: Stopped by a person
- **WHEN** a person stopped `add-login` on `studio`, so its claim is parked
- **THEN** dispatch does not continue it, and reports nothing new for it

### Requirement: Order of a pass
A pass SHALL take continuations before new changes, each in admission order, and stop at the fleet limit. A continuation SHALL go only to its holder's machine, which SHALL answer the report and have every agent the run's steps use; a run that was dispatched SHALL also need a free fleet slot there. The started command SHALL make the claim's admission check itself.

#### Scenario: Continuations first
- **WHEN** `max_runs = 2`, one claim is active, `add-login` is ready on `studio` with group 2 unticked, and approved `add-users` has no claim
- **THEN** the pass continues `add-login` on `studio` and does not place `add-users`, which waits for the next pass

#### Scenario: Holder unreachable
- **WHEN** `add-login` is ready on `studio` and `studio` doesn't answer the report
- **THEN** dispatch names `studio` as unreachable, does not continue `add-login` elsewhere, and goes on with other changes

### Requirement: Candidate machines
A machine SHALL be a candidate for a change only if it answers the report in time, has free fleet capacity, has a checkout of the repository's remote, and has every agent the change's steps use installed and not signed out. This machine SHALL be a candidate too, by the same rules.

#### Scenario: Agent signed out
- **WHEN** the change's build step uses Codex and Codex is signed out on `mini`
- **THEN** `mini` is not a candidate, and the output says `mini: codex signed out`

#### Scenario: Sign-in unknown
- **WHEN** an agent's sign-in on `lab` is `unknown`
- **THEN** `lab` stays a candidate and the output notes the unknown sign-in

### Requirement: Choosing a machine
Among candidates, dispatch SHALL prefer the change's `machines` list in its order, then the machine with the most free capacity, then the name in alphabetical order. Each placement SHALL reduce that machine's free capacity for the rest of the pass. A change with no candidate SHALL be left for the next pass and say why.

#### Scenario: Preferred machine full
- **WHEN** change `add-login` prefers `mini`, `mini` has no free capacity, and `lab` and `studio` have 2 and 1 free
- **THEN** dispatch places `add-login` on `lab`

### Requirement: Starting on a machine
Dispatch SHALL start a placed change by running `tuckerbox run --for <remote> --dispatched <change>` on the chosen machine, over SSH or herdr-core as its entry says, or locally for this machine. `--for` SHALL find that machine's checkout of the remote. The started run SHALL take a fleet slot and claim the change itself.

#### Scenario: Start over SSH
- **WHEN** dispatch places `add-login` on `mini`, whose entry is `ssh = "mini"`
- **THEN** it runs `ssh mini -- tuckerbox run --for <remote> --dispatched add-login`, and the run on `mini` takes a fleet slot and claims the change

### Requirement: Placement is advice
A placement SHALL NOT reserve anything. If the started run's claim is refused, or the remote command fails, dispatch SHALL report it and SHALL NOT retry on another machine in the same pass.

#### Scenario: Two dispatchers
- **WHEN** dispatch runs on `studio` and `mini` at once and both place `add-login`
- **THEN** one run claims it, the other refuses with `claimed by`, and both dispatch outputs say what happened

### Requirement: Dry run
`tuckerbox dispatch --dry-run [--json]` SHALL print the placement it would make, with every candidate's reasons, and start nothing and write nothing.

#### Scenario: Dry run
- **WHEN** the user runs `tuckerbox dispatch --dry-run`
- **THEN** it prints each change with its chosen machine or why none fits, and the fleet ref is unchanged
