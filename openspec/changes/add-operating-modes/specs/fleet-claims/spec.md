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
`tuckerbox run --take-over <change>` SHALL claim a change whose claim expired, with a new epoch, and start it on this machine. A parked claim SHALL also need `--force`. Work on the old machine SHALL be left untouched. Without a remote change branch, the run SHALL start at the first step from the base branch.

#### Scenario: Take over before shipping
- **WHEN** `mini`'s claim on `add-login` expired during its build step, no branch was pushed, and the user runs `tuckerbox run --take-over add-login` on `studio`
- **THEN** `studio` holds the claim with the next epoch, starts at the first step from the base branch, and `mini`'s worktree is unchanged

#### Scenario: Parked without force
- **WHEN** the claim on `add-login` is parked on `mini` and the user runs `tuckerbox run --take-over add-login` on `studio`
- **THEN** it refuses, says the claim is parked, and says `--force` takes it anyway

### Requirement: Take-over moves the branch
When the change's remote branch exists, the take-over claim SHALL be one atomic push that leases the fleet ref and the branch at the tip just fetched, and adds an empty `Take over <change> from <machine>` commit on that tip. If either lease fails, nothing SHALL change. Model: `formal/fleet.tla`, `FencedMerge`.

#### Scenario: Branch moved during take-over
- **WHEN** `studio` fetched `add-login`'s branch at `abc123` and `mini` pushes a fix before `studio`'s take-over push
- **THEN** `studio`'s push is refused, neither ref changes, and `studio` fetches and decides again

### Requirement: Fenced branch pushes
In fleet mode, every push of the change's branch, the first and each fix round's, SHALL be one `git push --atomic` with a lease on the branch and a lease on the fleet ref that renews this run's claim. If either lease fails, nothing SHALL be pushed and the run SHALL be `blocked`. Model: `formal/fleet.tla`, `FencedPush`.

#### Scenario: Old holder wakes before its first push
- **WHEN** `mini` slept during its steps, `studio` took over `add-login`, and `mini` then wakes and reaches its ship stage
- **THEN** `mini`'s push is refused, it creates no branch, and its run is `blocked` with `claim taken by studio`

#### Scenario: Old holder pushes a CI fix after take-over
- **WHEN** `mini` pushed `add-login`, slept while CI ran, `studio` took it over, and `mini` wakes and pushes a fix
- **THEN** the push is refused, the branch keeps `studio`'s take-over commit, and `mini` is `blocked` with `claim taken by studio`

### Requirement: Fenced merges
In fleet mode, a merge SHALL be pinned to the commit seen green, as in `forge-shipping`, and the run SHALL renew its claim just before merging and not merge if the claim is gone. Because every take-over of a branch moves its tip, a merge pinned by the old holder SHALL be refused by the forge. Model: `formal/fleet.tla`, `FencedMerge`.

#### Scenario: Old holder merges after take-over
- **WHEN** `mini` saw `add-login`'s head `abc123` green, slept, `studio` took it over, and `mini` then asks the forge to merge `abc123`
- **THEN** the forge refuses because the tip is `studio`'s take-over commit, nothing merges, and `mini` is `blocked`

### Requirement: Forge claim after a take-over
In fleet mode, the claim marker SHALL also carry `epoch=<n>`. A run whose fleet claim has a higher epoch than an open merge request's marker, and whose push last moved the branch, SHALL hold the forge claim: it SHALL rewrite the marker to its machine, run and epoch. This narrows `ship-claim`'s `Losing the claim race`, `Resuming a claim` and `Claims end with the merge request`, and `forge-shipping`'s `Starting a shipping run`, in fleet mode.

#### Scenario: Marker of the old holder
- **WHEN** `studio` took over `add-login` with epoch 2 and its merge request's marker says `machine=mini epoch=1`
- **THEN** `studio` rewrites the marker to `machine=studio epoch=2` and keeps shipping, and no run refuses it as `claimed by mini`

#### Scenario: Crash before the marker is rewritten
- **WHEN** `studio`'s take-over push succeeded and its process died before rewriting the marker
- **THEN** `tuckerbox resume add-login` on `studio` finds the tip is its push and its epoch is higher, rewrites the marker and continues

### Requirement: Shipping continues after a take-over
A take-over of a change whose branch exists SHALL make the worktree from the take-over commit and continue in the ship stage: no workflow step runs again. If the branch already holds the change's archive, the run SHALL NOT archive again. If no merge request is open, it SHALL open one with its marker.

#### Scenario: Take-over while waiting for CI
- **WHEN** `mini` pushed `add-login` with its archive commit, opened the merge request, and lost power while CI ran; the claim expired and the user ran `tuckerbox run --take-over add-login` on `studio`
- **THEN** `studio` pushes its take-over commit, rewrites the marker, polls CI for that commit, fixes or merges as `forge-shipping` says, and `mini`'s later pushes and merge are refused

#### Scenario: Branch without a merge request
- **WHEN** `mini` pushed `add-login` and died before opening the merge request, and `studio` takes it over
- **THEN** `studio` opens the merge request with its own marker after the take-over push, without archiving again

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
