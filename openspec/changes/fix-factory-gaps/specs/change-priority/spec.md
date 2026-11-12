# Spec Delta

## MODIFIED Requirements

### Requirement: Priority order
The priority order SHALL list the active changes by priority, then age (`created` in the change's `.openspec.yaml`, oldest first), then change id. It orders the list and the board, not admission. Tuckerbox SHALL read priority values as `priority-publish` says: from the priority ref, else from the repository checkout on this machine, committed or not. Its position SHALL be counted from 1.

#### Scenario: Ranked and unranked
- **WHEN** A has priority 2000, B has priority 1000, and C and D have none, with D older than C
- **THEN** the order is B, A, D, C

### Requirement: Where priority is written
In solo and fleet mode, `tuckerbox priority` SHALL write only the priority ref, as `priority-publish` says, and SHALL NOT write the checkout or a worktree. In team mode it SHALL write in the repository checkout, never in a change's worktree, change only the `priority` line, keeping other keys, tables and comments, and SHALL NOT commit, saying so. It SHALL NOT take the change's run lock, and SHALL work while the change is queued or running.

#### Scenario: Reorder a queued change
- **WHEN** `add-login` is queued and the user runs `tuckerbox priority add-login top`
- **THEN** the priority ref gets `add-login`'s new value, its worktree and the checkout are not touched, and the run's queue place follows the new order

#### Scenario: Reorder in team mode
- **WHEN** `mode = "team"` and the user runs `tuckerbox priority add-login top`
- **THEN** the checkout's `tuckerbox.toml` for `add-login` gets its new `priority` and the output says it is not committed

### Requirement: Priority keeps approval
A priority change SHALL NOT change a change's approval state on any machine. `tuckerbox approve` and `unapprove` SHALL keep the `priority` key.

#### Scenario: Reorder an approved change
- **WHEN** an approved change is moved to the top and the move is published
- **THEN** every machine reports its approval as `approved`
