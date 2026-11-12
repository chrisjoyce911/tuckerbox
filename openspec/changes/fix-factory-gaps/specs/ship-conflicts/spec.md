# Spec Delta

## Purpose

Defines how a shipping run notices that its merge request conflicts with the base branch and resolves it, instead of waiting for CI that will never run.

## ADDED Requirements

### Requirement: Detecting a conflict
On every poll of a shipping run, in `mr` and `merge` mode, Tuckerbox SHALL read the merge request's merge state before its CI: on GitHub `mergeable` and `mergeable_state`, on GitLab `has_conflicts` and `detailed_merge_status`. A merge state not yet computed SHALL be polled again. A conflicting merge request SHALL be handled as below, whatever its CI state, and SHALL NOT count toward `wait_timeout`.

#### Scenario: Conflicting pull request on GitHub
- **WHEN** the base moved, GitHub reports `mergeable: false` for `add-login`'s pull request, and no check runs on its head
- **THEN** the run does not wait for CI and starts a conflict round

#### Scenario: Not computed yet
- **WHEN** GitHub reports `mergeable: null`
- **THEN** the run polls again and does not start a conflict round

### Requirement: Conflict round
A conflict round SHALL take a parallel slot, fetch the base and merge it into the change's branch in its worktree with `git merge`, never rebasing. A clean merge SHALL be committed. Otherwise the fix step SHALL run with the conflicting files and both sides as its findings, and SHALL resolve and commit the merge. This SHALL replace the conflict rule of `Branch behind the base`.

#### Scenario: Clean merge
- **WHEN** the forge reports a conflict that merging the current base resolves without conflict
- **THEN** the merge is committed and no fix step runs

### Requirement: Checking and pushing a resolved merge
After a conflict round, Tuckerbox SHALL check that no path is unmerged, no conflict marker is left in the changed files, and the new commit has the base as a parent. It SHALL then push with the lease as `Push safety` says, free the slot and wait for CI on the new head.

#### Scenario: Fixer resolves the conflict
- **WHEN** merging `main` into `add-login` conflicts in `README.md` and the fix step resolves it
- **THEN** the merge commit is pushed with the lease and the run waits for CI on it

#### Scenario: Marker left behind
- **WHEN** the fix step commits the merge with `<<<<<<<` still in `README.md`
- **THEN** nothing is pushed, the merge is undone, and the run is `blocked` with reason `merge conflict: README.md`

### Requirement: Conflict rounds are bounded
Each conflict round SHALL count as one ship round against `[shipping] max_rounds`. When the limit is reached the run SHALL be `blocked` with reason `ship limit reached` instead of starting another round.

#### Scenario: Base keeps moving
- **WHEN** `max_rounds = 3`, the run has had two fix rounds, and the merge request conflicts again
- **THEN** one conflict round runs; if it conflicts once more the run is `blocked` with reason `ship limit reached`

### Requirement: A conflict the fixer can't resolve
When the fix step ends without a resolved merge commit, Tuckerbox SHALL abort the merge, push nothing, and mark the run `blocked` with reason `merge conflict: <files>`. The card SHALL show `needs_you` with that reason.

#### Scenario: Fixer gives up
- **WHEN** the fix step stops with `go.mod` still conflicting
- **THEN** the merge is aborted, nothing is pushed, and the card needs you with `merge conflict: go.mod`
