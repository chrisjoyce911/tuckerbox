# Spec Delta

## Purpose

Defines where priority moves are stored so that reordering a change takes effect on every machine at once, without touching the checkout, and how the order reads them.

## ADDED Requirements

### Requirement: Priority ref
In solo and fleet mode, a priority move (`tuckerbox priority <change> <n|up|down|top>` or the board) SHALL record the resulting stored values in `priority.toml` in a new commit on `refs/tuckerbox/priority`, whose parent is the version it fetched. It SHALL NOT write any file in the checkout or a worktree.

#### Scenario: Move from the board
- **WHEN** the owner drags `add-login` to the top on the board
- **THEN** `refs/tuckerbox/priority` has a new commit whose `priority.toml` puts `add-login` first, and `git status` in the checkout is clean

### Requirement: Moves published at once
Each move SHALL push its commit at once with `--force-with-lease` on the version it fetched, using the same publish routine as the approvals ref. When the push is refused because the ref moved, it SHALL fetch, plan the same move (change and target position) again on the new order, and push again. No accepted move SHALL be lost.

#### Scenario: Two moves at once
- **WHEN** the owner moves `add-login` to the top on `studio` while moving `add-users` to position 2 on `mini`
- **THEN** one push is refused, that machine fetches, re-plans its move on the order the other published, and pushes, and the ref holds both moves

### Requirement: Reading priority
The priority order, admission order and `board --json` SHALL read stored values from the priority ref, fetched from the remote (the board at most once a minute, cached). An entry on the ref SHALL win; without one, the `priority` in the change's `tuckerbox.toml` in the checkout SHALL count as before.

#### Scenario: Moved on one machine, seen on another
- **WHEN** the owner moves `add-login` to the top on `studio`
- **THEN** `tuckerbox priority` on `mini` lists `add-login` first after its next fetch, with no commit or pull

#### Scenario: Old value in the sidecar
- **WHEN** `add-users` has `priority = 500` in its sidecar and no entry on the ref
- **THEN** it is ordered by 500

### Requirement: Moving offline
When the push fails for want of the remote, the commit SHALL stay on the local ref and count on this machine, and the move SHALL be remembered in the state directory. The next move or board fetch SHALL re-plan each remembered move on the remote's version and push. Until then the board SHALL show `not published` and the command SHALL say so. A repository with no remote SHALL keep the ref locally and show no publishing state.

#### Scenario: Reorder on a train
- **WHEN** the owner moves `add-login` to the top with no network
- **THEN** the order changes on this machine, the board says `not published`, and the next board fetch with a network publishes it

### Requirement: Team mode writes the sidecar
In team mode, a move SHALL write the change's `tuckerbox.toml` in the checkout as `change-priority` describes, and the priority ref SHALL NOT count.

#### Scenario: Ref entry in a team repository
- **WHEN** `mode = "team"` and the priority ref puts `add-login` first
- **THEN** the order ignores the ref and follows the sidecars

### Requirement: Uncommitted priorities in the checkout
`tuckerbox check` SHALL warn, naming the files, when the checkout has uncommitted `priority` changes in sidecars outside team mode. The first move published after upgrade SHALL include the whole current order, uncommitted checkout values included, and say the checkout lines can be discarded.

#### Scenario: Leftover edits
- **WHEN** the checkout has an uncommitted `priority` change in `add-login`'s sidecar
- **THEN** `tuckerbox check` warns and names that file
