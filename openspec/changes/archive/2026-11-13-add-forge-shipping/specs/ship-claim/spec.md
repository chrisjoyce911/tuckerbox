# Spec Delta

## Purpose

Defines how one machine claims a change on the forge when it ships it, so that two machines reaching the end of the same change at once never both ship it or overwrite each other's work.

## ADDED Requirements

### Requirement: The claim is the branch and its merge request
A machine SHALL hold the claim on a change when its own lease push created or last moved the change's remote branch, and the open merge request for that branch carries its claim marker. Only the machine holding the claim SHALL push to the branch, reply on its merge request, or merge it. Model: `formal/ship.tla`, `OneClaim`.

#### Scenario: Two machines finish at once
- **WHEN** machines `studio` and `mini` both finish change `add-login` and push its branch at the same moment
- **THEN** one push creates the branch, the other is refused by its lease, and only the first machine opens the merge request

### Requirement: Claim marker
The merge request body SHALL carry a hidden marker naming the change, the machine and the run (`<!-- tuckerbox: change=<id> machine=<name> run=<id> -->`). The machine name SHALL come from machine config `machine`, else the host name. Tuckerbox SHALL read the marker to decide whose claim an open merge request is.

#### Scenario: Marker in the body
- **WHEN** machine `studio` opens the merge request for `add-login`
- **THEN** its body has the marker with `change=add-login` and `machine=studio`, and the rendered page does not show it

### Requirement: Losing the claim race
When a lease push is refused, the branch tip is not the run's last push, or opening the merge request finds an open one whose marker names another machine or none, the run SHALL become `blocked` with reason `claimed by <machine>` or `branch changed on the forge`, and the link when a merge request exists. It SHALL push nothing more. Model: `formal/ship.tla`, `NoLostPush`.

#### Scenario: Winning push precedes its merge request
- **WHEN** the losing run sees an existing branch before the winning merge request is visible
- **THEN** it is blocked with a branch-race reason (`remote branch exists` or `branch changed on the forge`), infers no owner without a marker, and pushes nothing more

#### Scenario: Merge request already open
- **WHEN** a run tries to open the merge request for `add-login` and the forge says one is open with `machine=studio` in its marker
- **THEN** the run on `mini` is `blocked` with reason `claimed by studio` and pushes nothing

#### Scenario: Branch moved by a person
- **WHEN** a person pushes to the change's branch while the run polls CI
- **THEN** on its next poll the run is `blocked` with reason `branch changed on the forge`, and any later push by it would be refused by its lease

### Requirement: Resuming a claim
After a crash or `tuckerbox resume`, a run SHALL keep its claim when the remote tip is the commit it last pushed and the open merge request (if any) carries its marker. It SHALL then open the merge request if none is open, or keep polling. When the remote tip contains its last push and a person added commits, resume SHALL fast-forward the worktree branch if the worktree is clean.

#### Scenario: Crash between push and open
- **WHEN** a run pushed its branch and its process died before opening the merge request
- **THEN** `tuckerbox resume` finds the tip is its own push, opens the merge request and continues

#### Scenario: Resume after a person's fix
- **WHEN** a run is `blocked` because a person pushed a fix on top of its last push, and its worktree is clean
- **THEN** `tuckerbox resume` fast-forwards the worktree branch to the remote tip and polls CI for it

### Requirement: Claims end with the merge request
A merged or closed merge request SHALL NOT be a claim. A change whose remote branch exists with no open merge request and a tip this run didn't push SHALL be refused with reason `remote branch exists`, and Tuckerbox SHALL NOT delete or overwrite that branch.

#### Scenario: Leftover branch
- **WHEN** an earlier, closed merge request left branch `add-login` on the forge and a new run reaches the ship stage
- **THEN** the run is `blocked` with reason `remote branch exists`, and the branch is unchanged
