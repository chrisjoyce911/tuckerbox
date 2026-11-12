# Spec Delta

## Purpose

Defines how the owner ranks OpenSpec changes, how Tuckerbox orders changes and queued runs by that rank, and how the board shows and changes the order.

## ADDED Requirements

### Requirement: Priority rank
A change MAY have `priority = <n>`, a positive integer, in its `tuckerbox.toml`. Lower SHALL come first. A change without one SHALL come after every ranked change. `tuckerbox check` SHALL fail on a value that is not a positive integer, naming the file. Ordering SHALL treat such a value as no priority.

#### Scenario: Invalid priority
- **WHEN** a change's `tuckerbox.toml` has `priority = "high"`
- **THEN** `tuckerbox check` fails and names that file and `priority`

### Requirement: Priority order
The priority order SHALL list the active changes by priority, then age (`created` in the change's `.openspec.yaml`, oldest first), then change id. It orders the list and the board, not admission. Tuckerbox SHALL read it from the repository checkout on this machine, committed or not. Its position SHALL be counted from 1.

#### Scenario: Ranked and unranked
- **WHEN** A has priority 2000, B has priority 1000, and C and D have none, with D older than C
- **THEN** the order is B, A, D, C

### Requirement: Listing the order
`tuckerbox priority` SHALL list the active changes in priority order, each with its position and stored value, or `-` for none. `--json` SHALL print the same as a document with a `version`.

#### Scenario: List
- **WHEN** the user runs `tuckerbox priority` with B at 1000 and A unranked
- **THEN** it prints B at position 1 with 1000, then A at position 2 with `-`

### Requirement: Moving a change
`tuckerbox priority <change> <n|up|down|top>` SHALL move the change to position `n`, one place up or down, or to position 1. An `n` past the end SHALL mean last. Tuckerbox SHALL choose the stored values. Changes above the new position that have no priority SHALL get one, in their current order. The other changes SHALL keep their order.

#### Scenario: Move to a position
- **WHEN** the order is A, B, C, D and the user runs `tuckerbox priority D 2`
- **THEN** the order is A, D, B, C

#### Scenario: Up at the top
- **WHEN** A is at position 1 and the user runs `tuckerbox priority A up`
- **THEN** nothing changes and it exits 0

#### Scenario: Unknown change
- **WHEN** the user runs `tuckerbox priority add-nothing top` and no active change has that id
- **THEN** it exits non-zero, names the id and writes nothing

### Requirement: Few files per move
A move SHALL rewrite only the moved change's `priority` when there is an integer gap between its new neighbours, and SHALL use gaps of 1000 for values it adds. Only when no gap is left SHALL it renumber the ranked changes, keeping their order.

#### Scenario: Gap available
- **WHEN** A has 1000, B has 2000, C has 3000 and the user moves C to position 2
- **THEN** only C's file changes, to a value between 1000 and 2000

#### Scenario: No gap left
- **WHEN** A has 1000, B has 1001 and the user moves C between them
- **THEN** the ranked changes are renumbered in the new order, A, C, B

### Requirement: Where priority is written
`tuckerbox priority` SHALL write in the repository checkout, never in a change's worktree. It SHALL change only the `priority` line, keeping other keys, tables and comments, and SHALL NOT commit, saying so. It SHALL NOT take the change's run lock, and SHALL work while the change is queued or running.

#### Scenario: Reorder a queued change
- **WHEN** `add-login` is queued and the user runs `tuckerbox priority add-login top`
- **THEN** the checkout's `tuckerbox.toml` for `add-login` gets its new `priority`, its worktree is not touched, and the output says it is not committed

### Requirement: Priority keeps approval
A priority change SHALL NOT change a change's approval state on any machine. `tuckerbox approve` and `unapprove` SHALL keep the `priority` key.

#### Scenario: Reorder an approved change
- **WHEN** an approved change is moved to the top, and the move is committed and pulled on a second machine
- **THEN** both machines report its approval as `approved`

### Requirement: One writer for the sidecar
Every Tuckerbox command that rewrites a change's `tuckerbox.toml` SHALL hold one repository-wide sidecar lock while it reads and writes it, and SHALL replace each file in one step. A `priority` move SHALL hold it until all its files are written. A second holder SHALL wait for the lock.

#### Scenario: Priority and approve at once
- **WHEN** `tuckerbox approve add-login` and `tuckerbox priority add-login top` run at the same time on a change without a worktree
- **THEN** the checkout's file ends with both the `[approval]` table and the new `priority`

### Requirement: Admission order
A change's admission rank SHALL be the lowest `priority` value of it and every active change that depends on it, directly or not, with no value after every value. Age SHALL NOT count. Changes SHALL be ordered one at a time: next is the change with the lowest rank, then the earliest request, then the lowest id, among those whose dependencies in the set are already placed.

#### Scenario: Dependency moves up
- **WHEN** B has priority 1000 and depends on A, A has none, and C has 2000
- **THEN** in admission order A comes before C

#### Scenario: Unranked changes keep request order
- **WHEN** A runs with `max_parallel = 1`, no change has a priority, and the user starts B and then C, where C is older than B
- **THEN** B starts before C

#### Scenario: Branching dependencies
- **WHEN** D has priority 1000 and depends on A and C, A depends on B, none of A, B and C has a priority, and they are requested in the order A, C, B, D
- **THEN** the admission order is C, B, A, D

### Requirement: Run all order
`tuckerbox run --all` SHALL treat its changes as requested oldest first (`created`, then id), and SHALL request them in admission order.

#### Scenario: Run all in order
- **WHEN** C has priority 1000, A 2000 and B none, and the user runs `tuckerbox run --all` with `max_parallel = 1`
- **THEN** C starts first, then A, then B

### Requirement: Queue follows the order
When a slot frees, the queued run first in admission order SHALL take it. Runs waiting on dependencies SHALL NOT be queued. A run SHALL choose the first run and try for a slot while holding the sidecar lock, which a `priority` move holds for all its writes and a run holds to join the queue. Edits made outside Tuckerbox SHALL count from the next admission. Model: `formal/priority.tla`, `OrderedAdmission`.

#### Scenario: Reorder while queued
- **WHEN** `max_parallel = 1`, A runs, B and then C are queued, and the user moves C to the top
- **THEN** C starts when A ends, before B

#### Scenario: Reorder as a slot frees
- **WHEN** B is first in the queue, A ends, and `tuckerbox priority C top` finishes before B takes the free slot
- **THEN** C takes the slot and B stays queued

#### Scenario: Higher-priority arrival during admission
- **WHEN** A holds the only slot, B finds itself first in the queue, and C, ranked above B, is started as A ends
- **THEN** either C joins before B's check and takes the slot, or C joins after B took it; B never takes the slot while C is queued

#### Scenario: Slot frees during a renumber
- **WHEN** a move renumbers five changes and a slot frees while it writes them
- **THEN** no run takes the slot until the move ends, and then the first run in the new order takes it

### Requirement: Priority on the board
Each active card SHALL include its `priority` value, or none, and its `position` in priority order. The Proposed, Approved and In progress columns, and in the steps layout Queued and each step column, SHALL list cards in priority order. Ready and Done SHALL keep their order. Positions SHALL come from the board's own machine.

#### Scenario: Board order
- **WHEN** two Approved cards, A with priority 2000 and B with 1000, are read
- **THEN** B is listed before A, with positions in priority order

### Requirement: Drag to reorder
In the board mod, the owner SHALL be able to drag a card within a sorted column, not into another column. A drop SHALL run `tuckerbox priority <change> <n>` on the board's own machine, with `n` the position that puts the card where it was dropped. The board SHALL show the command's error, and the order SHALL change only when the next board read shows it.

#### Scenario: Drag above a card
- **WHEN** the owner drags D, at position 5, above B, at position 2, in the Approved column
- **THEN** the board runs `tuckerbox priority D 2`, and D is listed above B on the next read

#### Scenario: Drag down
- **WHEN** the owner drags A, at position 1, below C, at position 3
- **THEN** the board runs `tuckerbox priority A 3`
