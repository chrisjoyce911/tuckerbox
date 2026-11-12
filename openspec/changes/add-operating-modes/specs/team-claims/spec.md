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
In team mode, repository config `[team] approvers` SHALL list forge logins. `tuckerbox approve` SHALL record the approver's forge login, read from the forge. `tuckerbox check` SHALL fail in team mode when approval is required and the list is empty.

#### Scenario: No approvers
- **WHEN** `mode = "team"`, approval is required and `[team] approvers` is missing
- **THEN** `tuckerbox check` exits non-zero and names `team.approvers`

### Requirement: Approvals only from the protected base
In team mode the run gate SHALL read the approval only from the base branch at the commit just fetched from the remote, never from a worktree, a retained change branch or the checkout. The approval's login SHALL be in `[team] approvers`, and the change's files the run will use SHALL match that approval's hashes. Otherwise the run SHALL refuse.

#### Scenario: Approval by someone not listed
- **WHEN** `[team] approvers = ["sam"]` and the base branch holds an approval of `add-login` by `alex`
- **THEN** `tuckerbox run add-login` refuses and says `alex` is not an approver

#### Scenario: Forged approval on a retained branch
- **WHEN** a local branch `add-login` has edited proposal text and an `[approval]` with recomputed hashes and `login = "sam"`, and the base branch has no approval for `add-login`
- **THEN** `tuckerbox run add-login` refuses, says the base branch has no approval, and creates no worktree, branch or merge request

#### Scenario: Worktree edited after approval
- **WHEN** the base branch holds `sam`'s approval of `add-login`, and its existing worktree has edited design text with an `[approval]` rewritten to match
- **THEN** `tuckerbox run add-login` refuses and says the worktree's files don't match the approval on the base branch

### Requirement: Resumed settings from the protected base
In team mode, `tuckerbox resume` and `tuckerbox rerun` SHALL compare the change's `agent` and `permissions` overrides they will use with the `settings_hash` of the approval on the base branch just fetched, whose login is listed, never with the worktree's own `[approval]`. On a mismatch they SHALL refuse. A stale text hash alone SHALL NOT stop them.

#### Scenario: Forged settings approval on resume
- **WHEN** a team run of `add-login` stopped, someone set `step.build.agent=codex` in its worktree and rewrote the worktree's `settings_hash` to match, and the base branch's approval covers `claude`
- **THEN** `tuckerbox resume add-login` refuses, names `step.build.agent`, and says the approval on the base branch doesn't cover it

#### Scenario: Forged settings approval on rerun
- **WHEN** the same forged worktree settings are in place and the user runs `tuckerbox rerun add-login build`
- **THEN** it refuses as resume does and starts no step

#### Scenario: Design rewritten by a plan step
- **WHEN** a team run's plan step edited the design and the run then blocked, with no settings changed
- **THEN** `tuckerbox resume` continues the run

### Requirement: The base branch must be protected
In team mode, `tuckerbox check` and `tuckerbox run` SHALL ask the forge whether the base branch refuses direct pushes and needs a merge request with an approving review. When it doesn't, or the forge can't say, `run` SHALL refuse with `team approvals need a protected base branch`, and `check` SHALL fail.

#### Scenario: Unprotected base
- **WHEN** `mode = "team"` and the forge allows direct pushes to `main`
- **THEN** `tuckerbox run` refuses with `team approvals need a protected base branch` and `tuckerbox check` exits non-zero

#### Scenario: Protection can't be read
- **WHEN** the token can't read the base branch's protection rules
- **THEN** `tuckerbox run` refuses, says the protection couldn't be confirmed, and names the permission the token needs

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
