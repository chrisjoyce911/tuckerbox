# change-approval Specification

## Purpose
Defines how a person approves an OpenSpec change before agents may start it, how that approval lapses when the change's text changes, and how runs are held back until a change is approved. The run gate itself is in `change-runs` (`Run gate`, `Gate at the start`) and the `require_approval` setting in `repo-config` (`Approval setting`).

## Requirements

### Requirement: Approving a change
`tuckerbox approve <change>` SHALL validate the change as `tuckerbox validate` does, and refuse an invalid change. It SHALL then write an `[approval]` table to the change's `tuckerbox.toml` with the approval hash, who approved (git `user.name`) and when (RFC 3339 with offset). It SHALL keep the file's other keys. It SHALL NOT commit, and SHALL say the approval only counts elsewhere once committed and pushed.

#### Scenario: Approve a valid change
- **WHEN** the user runs `tuckerbox approve add-login` on a valid change whose `tuckerbox.toml` has `depends_on = ["add-users"]`
- **THEN** the file gains an `[approval]` table, still has `depends_on = ["add-users"]`, and the output says the approval is not committed

#### Scenario: Approve an invalid change
- **WHEN** the user runs `tuckerbox approve add-login` and strict validation fails
- **THEN** it exits non-zero, shows the errors and leaves `tuckerbox.toml` unchanged

### Requirement: Approval hash
The approval hash SHALL be a SHA-256 over proposal.md, design.md, model.md, tasks.md and every spec delta under `specs/`, each with its path, in path order. Line endings SHALL be read as LF, and every task checkbox in tasks.md as unchecked. A missing file SHALL hash differently from an empty one. `tuckerbox.toml` SHALL NOT be part of the hash.

#### Scenario: Ticking a task keeps approval
- **WHEN** an approved change has task 1.1 ticked
- **THEN** its approval state is still `approved`

#### Scenario: Editing a task's text voids approval
- **WHEN** an approved change has the wording of task 1.1 changed
- **THEN** its approval state is `stale`

#### Scenario: Line endings
- **WHEN** a Windows checkout has the change's files with CRLF line endings
- **THEN** the hash equals the hash of the same files with LF line endings

### Requirement: Approval state
A change's approval state SHALL be `approved` when its `[approval]` hash matches its files, `stale` when it has an approval that no longer matches, and `none` when it has no approval. Approval SHALL lapse by itself: no command is needed to void it, and every machine that has the same files reaches the same state.

#### Scenario: Edit pulled on another machine
- **WHEN** a change approved and pushed from one machine has its design edited, committed and pulled on a second machine
- **THEN** both machines report the change's approval as `stale`

### Requirement: Withdrawing approval
`tuckerbox unapprove <change>` SHALL remove the `[approval]` table from the change's `tuckerbox.toml`, keep the file's other keys, and not commit. Unapproving a change that has no approval SHALL change nothing and exit 0.

#### Scenario: Unapprove
- **WHEN** the user runs `tuckerbox unapprove add-login` on an approved change
- **THEN** its approval state becomes `none` and the output says the change is not committed
