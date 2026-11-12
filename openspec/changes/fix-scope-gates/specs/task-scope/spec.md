# Spec Delta

## ADDED Requirements

### Requirement: CI tasks
A task whose text starts with `(ci)`, or says it passes or is green on or in CI, SHALL be a CI task unless it is a ship task. With a ship mode other than `local`, an unticked CI task SHALL NOT block a scope's run and SHALL NOT count when deciding whether the change is complete. In `local` mode it SHALL count like any other task. Only with a ship mode other than `local` SHALL the step brief say not to tick CI tasks.

#### Scenario: CI task in local mode
- **WHEN** `ship = "local"` and a run with scope `1` leaves `1.2 (ci) Release archives build` unticked
- **THEN** the run is `blocked` with `scope tasks not ticked: 1.2`

#### Scenario: Brief in local mode
- **WHEN** `ship = "local"` and the run's scope has `1.2 Go tests pass on Linux, macOS and Windows CI`
- **THEN** the step brief doesn't say to leave 1.2 unticked, and a builder that ticks it makes the run `ready` and complete

### Requirement: Shipping ticks CI tasks
Shipping SHALL tick the change's CI tasks once CI is green on the merge request, commit them as `Tick <change> task N.M`, push that commit to the merge request, and wait for CI on it before reporting ready or merging. While CI is not green, CI tasks SHALL stay unticked.

#### Scenario: Final integration task passes on CI
- **WHEN** `ship = "mr"`, group 6 has `6.1 Wire it up` and `6.2 Go tests pass on Linux, macOS and Windows CI`, and a run with scope `6` ticks only 6.1
- **THEN** the run ships, and once CI is green on the merge request shipping ticks 6.2 in a commit `Tick add-login task 6.2` pushed to the merge request

#### Scenario: CI fails
- **WHEN** shipping a change with an unticked `(ci)` task and CI on the merge request is red
- **THEN** the `(ci)` task stays unticked

### Requirement: Change archived on its branch
When a run's last step is done and the change's folder is gone from its worktree while the worktree's `openspec/changes/archive` has the change's folder (`<date>-<change>`), the run SHALL read tasks from the archived `tasks.md`, SHALL count the change as complete, and SHALL ship as its mode says. Shipping SHALL record the archived folder and SHALL NOT archive the change again.

#### Scenario: Builder archived the change
- **WHEN** a builder moves `openspec/changes/add-priority` to `openspec/changes/archive/2026-11-12-add-priority` on the branch and review passes
- **THEN** the run is not blocked for scope tasks missing from `tasks.md`, and it is `ready` and complete in local mode or ships in `mr` and `merge` modes

#### Scenario: Not archived twice
- **WHEN** `shipping.archive = true` and the change is already archived on its branch
- **THEN** shipping doesn't run `openspec archive` and doesn't commit `Archive <change>`
