# approval-publish Specification

## Purpose
Defines where Tuckerbox keeps approvals so that approving a change takes effect on every machine at once, without a commit to the base branch, and how the run gate and the board read them.

## Requirements

### Requirement: Approvals ref
In solo and fleet mode, `tuckerbox approve` and `unapprove` SHALL record the approval (hashes, who, when) or its withdrawal as `approvals/<change>.toml` in a new commit on `refs/tuckerbox/approvals`, holding the change lock as today. They SHALL NOT write the change's `tuckerbox.toml`.

#### Scenario: Approve from the board
- **WHEN** the owner clicks Approve on `add-login`'s card
- **THEN** `refs/tuckerbox/approvals` has a new commit whose `approvals/add-login.toml` holds the approval, and no file in the checkout changed

### Requirement: Published at once
Each approve or unapprove SHALL push its commit at once with `--force-with-lease` on the ref's version it fetched. When the push is refused because the ref moved, it SHALL fetch, apply its one change to the new version, and push again. No accepted approval or withdrawal SHALL be lost. Model: `formal/approvals.tla`, `NoLostApproval`.

#### Scenario: Two approvals at once
- **WHEN** the owner approves `add-login` on `studio` while approving `add-users` on `mini` at the same moment
- **THEN** one push is refused, that machine fetches and pushes again, and the ref holds both approvals

### Requirement: Reading approvals
The run gate and `board --json` SHALL read a change's approval from the approvals ref, fetched from the remote; the gate fetches again under the change lock before its second judgment. An entry on the ref SHALL win. Without one, an `[approval]` in the change's `tuckerbox.toml` SHALL count as before. A withdrawal SHALL mean `none`.

#### Scenario: Approved on one machine, run on another
- **WHEN** the owner approves `add-login` on `studio` and then runs `tuckerbox run add-login` on `mini`
- **THEN** `mini`'s run gate fetches the ref, finds the approval, and starts the run with no commit to the base branch

#### Scenario: Old approval in the sidecar
- **WHEN** `add-users` has an `[approval]` in its `tuckerbox.toml` on the base branch and no entry on the ref
- **THEN** its approval state is judged from that table, as before

#### Scenario: Unapproved after an old approval
- **WHEN** `add-users` has an old `[approval]` in its `tuckerbox.toml` and the owner runs `tuckerbox unapprove add-users`
- **THEN** the ref holds a withdrawal for `add-users` and its approval state is `none`

### Requirement: Approval still covers the text
An approval on the ref SHALL hold the same hashes as before (the change's text, and its settings), and SHALL be judged against the files the run will use, as `change-approval` says. An edit after approval SHALL make it `stale`.

#### Scenario: Edited after approval
- **WHEN** `add-login` was approved on the ref and its design is then edited and merged to the base branch
- **THEN** its approval state is `stale` on every machine

### Requirement: Approving offline
When the push fails for want of the remote, the commit SHALL stay on the local ref and count on this machine. The next approve, unapprove or board fetch SHALL push it, applying its changes on top of the remote's version. Until then the card SHALL show `not published`, and the command SHALL say so.

#### Scenario: Approve on a train
- **WHEN** the owner approves `add-login` with no network
- **THEN** the approval counts on this machine, the card says `not published`, and the next board fetch with a network publishes it

### Requirement: Publishing state on cards
Each card's approval SHALL include `published`: `false` when this machine has an approve or unapprove for the change not yet on the remote, else `true`. A repository with no remote SHALL keep its approvals ref locally and show no publishing state.

#### Scenario: Card after publishing
- **WHEN** an approval was pushed to the remote
- **THEN** the card's approval has `published: true` and shows no `not published` note

### Requirement: Team mode keeps the protected base
In team mode, `approve` and `unapprove` SHALL write the change's `tuckerbox.toml` as before, and the run gate SHALL read approvals only from the protected base branch, as `add-operating-modes` requires. The approvals ref SHALL NOT count.

#### Scenario: Ref entry in a team repository
- **WHEN** `mode = "team"` and someone pushed an approval for `add-login` to `refs/tuckerbox/approvals`
- **THEN** the run gate ignores it and refuses unless the protected base holds a listed approver's approval
