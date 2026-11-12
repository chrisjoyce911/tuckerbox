# Spec Delta

## Purpose

Defines how several people share one repository: the forge claim from shipping becomes the lock on a change from the start of its run, cards show who holds each change, and the repository says whose approval counts.

## ADDED Requirements

### Requirement: Claim at the start
In team mode, `tuckerbox run` SHALL, after the run gate and before any agent starts, commit an empty `Start <change>` commit on the change's new branch, push it with the lease that expects no branch, and open a draft merge request carrying the claim marker. It SHALL follow `ship-claim`'s rules. Model: `formal/ship.tla`, `OneClaim` and `NoLostPush`.

#### Scenario: Two people start one change
- **WHEN** two people run `tuckerbox run add-login` at the same moment on their own machines
- **THEN** one push creates the branch and opens the draft, the other is refused by its lease, and that person's run refuses with `claimed by` the first person and machine

### Requirement: Owner in the claim
In team mode, the claim marker SHALL also name the forge login of the token that opened the merge request, as `by=<login>`. Tuckerbox SHALL read the login from the forge, never from git config. A marker whose `by` differs from the merge request's author SHALL be treated as someone else's claim.

#### Scenario: Marker with the owner
- **WHEN** login `sam` starts `add-login` on machine `studio`
- **THEN** the draft's marker has `change=add-login`, `machine=studio` and `by=sam`

#### Scenario: Marker copied by someone else
- **WHEN** login `alex` opens a merge request whose marker says `by=sam`
- **THEN** no run treats it as `sam`'s claim, and the card shows the owner as `alex`

### Requirement: Ready at the ship stage
In team mode, the ship stage SHALL find the draft merge request its run opened, update its title and body as `forge-shipping` describes for a new one, and mark it ready for review, instead of opening another. This narrows `forge-shipping`'s `Opening the merge request` for team mode.

#### Scenario: Last step done in team mode
- **WHEN** the last step of `add-login` is done and its draft merge request is open with this run's marker
- **THEN** the merge request is updated and marked ready, and no second one is opened

### Requirement: Handing a change over
In team mode, a claim SHALL last until its merge request is merged or closed, also while the run is stopped or blocked. `--take-over` SHALL be refused. A person hands a change over by closing its merge request and deleting its branch, after which `ship-claim`'s rules let another person start it.

#### Scenario: Take over refused
- **WHEN** the user runs `tuckerbox run --take-over add-login` in a team repository
- **THEN** it refuses and says to close the merge request and delete the branch to hand the change over

### Requirement: Approvers
In team mode, repository config `[team] approvers` SHALL list forge logins. `tuckerbox approve` SHALL record the approver's forge login. With `require_approval = true`, the run gate SHALL count an approval only when its login is listed. `tuckerbox check` SHALL fail in team mode when approval is required and the list is empty.

#### Scenario: Approval by someone not listed
- **WHEN** `[team] approvers = ["sam"]` and `alex` approved `add-login`
- **THEN** `tuckerbox run add-login` refuses and says `alex` is not an approver

#### Scenario: No approvers
- **WHEN** `mode = "team"`, approval is required and `[team] approvers` is missing
- **THEN** `tuckerbox check` exits non-zero and names `team.approvers`

### Requirement: No dispatch in team mode
In team mode, `tuckerbox dispatch` SHALL refuse and say each person starts their own runs. Tuckerbox SHALL NOT start a run on another person's machine.

#### Scenario: Dispatch in a team repository
- **WHEN** the user runs `tuckerbox dispatch` with `mode = "team"`
- **THEN** it exits non-zero and starts nothing

### Requirement: Claims for the board
In team mode, `tuckerbox board --json` SHALL read the open merge requests that carry claim markers from the forge, at most once a minute per repository, keep the answer in the state directory, and use the kept answer otherwise. When the forge can't be reached, it SHALL use the kept answer and say how old it is.

#### Scenario: Board read twice
- **WHEN** the board reads a team repository twice within 30 seconds
- **THEN** only the first read asks the forge, and both show the same owners
