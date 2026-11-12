# Spec Delta

## Purpose

Defines how machines in a fleet repository claim a change before working on it, so that two machines never both work on or ship one change, even when a machine crashes, sleeps or comes back late.

## ADDED Requirements

### Requirement: The fleet ref
Fleet claims SHALL live in one ref on the repository's remote, `refs/tuckerbox/fleet`: a commit holding one record per claimed change with its machine, run id, epoch, state and last renewal time. Tuckerbox SHALL update it only by a push with `--force-with-lease` on the version it last fetched, and fetch and retry when that push is refused.

#### Scenario: Two machines write at once
- **WHEN** `studio` and `mini` both push a new fleet ref built on the same fetched version
- **THEN** one push is accepted, the other is refused by its lease, and that machine fetches the new version before deciding again

### Requirement: Claiming at run start
In fleet mode, `tuckerbox run` SHALL claim the change after the run gate and before it makes a worktree or run state. A change whose claim is held by another machine and not expired SHALL be refused with `claimed by <machine>`. Each claim SHALL carry an epoch one higher than the change's last. Model: `formal/fleet.tla`, `OneHolderPerEpoch`.

#### Scenario: Two machines pick the same change
- **WHEN** `studio` and `mini` both run `add-login` at the same moment and both see it unclaimed
- **THEN** exactly one claims it and starts; the other refuses with `claimed by` that machine and makes no worktree

#### Scenario: Claimed elsewhere
- **WHEN** `mini` holds a live claim on `add-login` and the user runs `tuckerbox run add-login` on `studio`
- **THEN** it refuses, names `mini` and the claim's state

### Requirement: Fleet limit
Repository config `[fleet] max_runs` SHALL limit how many changes hold an active or parked claim at once, across all machines. A claim that would go over it SHALL be refused with `fleet limit reached`. Without `max_runs` there SHALL be no fleet limit. Model: `formal/fleet.tla`, `WithinFleetLimit`.

#### Scenario: Limit reached
- **WHEN** `max_runs = 2`, two changes hold claims, and a third change is run on any machine
- **THEN** the third refuses with `fleet limit reached` and names the two claimed changes

### Requirement: Renewing a claim
While its run is queued, running or shipping, the run's own process SHALL renew its claim at least every fifth of `[fleet] lease` (10 minutes by default). When a renewal finds the claim is no longer its own, the run SHALL stop its agent, push nothing more, and be `blocked` with `claim taken by <machine>`.

#### Scenario: Claim taken while running
- **WHEN** `studio`'s run of `add-login` renews and finds `mini` took the change over
- **THEN** `studio` stops its agent and its run is `blocked` with reason `claim taken by mini`

### Requirement: Parked claims
When a run is stopped, blocked or failed, it SHALL mark its claim `parked`. A parked claim SHALL NOT expire and SHALL count toward the fleet limit. `tuckerbox resume` and `rerun` SHALL make it active again with a lease push, and refuse if it is no longer theirs.

#### Scenario: Blocked overnight
- **WHEN** a run of `add-login` on `studio` is blocked at 18:00 and nobody resumes it until the next morning
- **THEN** its claim is still `parked` on `studio`, and no other machine claims the change

### Requirement: Expired claims
An active claim not renewed within `[fleet] lease` SHALL be `expired`. Tuckerbox SHALL NOT remove an expired claim by itself. It SHALL show it with its machine and last renewal time, and `tuckerbox run` SHALL refuse the change and say how to take it over.

#### Scenario: Machine crashed
- **WHEN** `mini` lost power while running `add-login` and 10 minutes passed
- **THEN** the claim shows `expired`, held by `mini`, with the last renewal time, and `tuckerbox run add-login` on `studio` refuses and suggests `--take-over`

### Requirement: Taking over a change
`tuckerbox run --take-over <change>` SHALL claim a change whose claim expired, with a new epoch, and start it on this machine. A parked claim SHALL also need `--force`. The work on the old machine SHALL be left untouched. The run SHALL start from the change's remote branch if it exists, else the base branch.

#### Scenario: Take over after a crash
- **WHEN** `mini`'s claim on `add-login` has expired and the user runs `tuckerbox run --take-over add-login` on `studio`
- **THEN** `studio` holds the claim with the next epoch, starts the run, and `mini`'s worktree is unchanged

#### Scenario: Parked without force
- **WHEN** the claim on `add-login` is parked on `mini` and the user runs `tuckerbox run --take-over add-login` on `studio`
- **THEN** it refuses, says the claim is parked, and says `--force` takes it anyway

### Requirement: Fenced first push
In fleet mode, the ship stage's first push of the change's branch SHALL renew the fleet claim in the same atomic push (`git push --atomic`), with a lease on both refs. If either lease fails, nothing SHALL be pushed and the run SHALL be `blocked`. Model: `formal/fleet.tla`, `FencedPush`.

#### Scenario: Old holder wakes up
- **WHEN** `mini` slept, `studio` took over `add-login`, and `mini` then wakes and reaches its ship stage first
- **THEN** `mini`'s push is refused, no branch is created by it, and its run is `blocked` with `claim taken by studio`

### Requirement: Claims after a run
A run that becomes `ready` SHALL mark its claim `ready`: it does not expire or count toward the fleet limit, and still refuses new claims. A run that becomes `merged` SHALL remove its claim. Any write of the fleet ref SHALL drop claims of changes archived on the base branch.

#### Scenario: Ready on one machine
- **WHEN** `add-login` is `ready` on `studio` in local ship mode and `tuckerbox dispatch` runs on `mini`
- **THEN** `add-login` is not placed, and the claim shows `ready` on `studio`

### Requirement: Showing and releasing claims
`tuckerbox claim show [--json]` SHALL list the fleet ref's claims with change, machine, state, epoch and last renewal. `tuckerbox claim release <change>` SHALL remove this machine's claim when no run is active on it. Removing another machine's claim SHALL need `--force`.

#### Scenario: Release another machine's claim
- **WHEN** the user runs `tuckerbox claim release add-login` on `studio` and the claim is `mini`'s
- **THEN** it refuses and says `--force` removes it
