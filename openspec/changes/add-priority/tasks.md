# Tasks

## 1. Rank and priority order (change-priority)

- [ ] 1.1 Read `priority` from each change's `tuckerbox.toml` in the repository checkout; `tuckerbox check` fails on a non-positive or non-integer value, naming the file
- [ ] 1.2 Priority order: priority, unranked last, then `created`, then id; positions from 1
- [ ] 1.3 Test: Invalid priority
- [ ] 1.4 Test: Ranked and unranked

## 2. The priority command (change-priority)

- [ ] 2.1 `tuckerbox priority [--json]` lists the order with position and value
- [ ] 2.2 `tuckerbox priority <change> <n|up|down|top>`: midpoint between new neighbours, gaps of 1000, rank unranked changes above the target, renumber only when no gap is left
- [ ] 2.3 Write only the `priority` line in the checkout (insert before the first table), keep keys and comments, never commit, say so; no run lock; refuse unknown, archived or missing change folders
- [ ] 2.4 Test: List
- [ ] 2.5 Test: Move to a position
- [ ] 2.6 Test: Up at the top
- [ ] 2.7 Test: Unknown change
- [ ] 2.8 Test: Gap available
- [ ] 2.9 Test: No gap left
- [ ] 2.10 Test: Reorder a queued change

## 3. Approval and the sidecar lock (change-priority)

- [ ] 3.1 `<common-dir>/tuckerbox/sidecar.lock` around every sidecar rewrite (`priority`, `approve`, `unapprove`, and `set` if `add-change-settings` is in), taken after any run lock; write a temporary file and rename it
- [ ] 3.2 `approve` and `unapprove` keep `priority`
- [ ] 3.3 Test: Reorder an approved change (hash fixture unchanged by a `priority` line)
- [ ] 3.4 Test: Priority and approve at once (concurrent processes)

## 4. Admission order (change-priority, change-runs)

- [ ] 4.1 Effective priority over the transitive dependents among active changes; sort key: effective priority, dependency first, ticket time (age for `--all`), id
- [ ] 4.2 `Queue.Head`: sort live tickets by the key, reading priorities and `depends_on` from the checkout on each check
- [ ] 4.3 `run --all` requests changes in admission order
- [ ] 4.4 Test: Dependency moves up
- [ ] 4.5 Test: Run all in order
- [ ] 4.6 Test: Reorder while queued
- [ ] 4.7 Test: More changes than slots
- [ ] 4.8 Test: Change already running
- [ ] 4.9 Test: Unranked changes keep request order

## 5. Board (change-priority)

- [ ] 5.1 `priority` and `position` on active cards; sort Proposed, Approved, In progress, Queued and step columns by priority order
- [ ] 5.2 Mod: drag within a sorted column only; compute the target position; run `tuckerbox priority <change> <n>` on the board's machine; show errors; show positions on cards
- [ ] 5.3 Test: Board order
- [ ] 5.4 Test: Drag above a card
- [ ] 5.5 Test: Drag down

## 6. Wrap-up

- [ ] 6.1 README: `priority`, the admission order, and that priorities are written in the checkout and not committed
- [ ] 6.2 `go vet ./...`, `go test -race ./...` and `scripts/tla-check.sh` pass
- [ ] 6.3 Archive after `add-herdr-board`, once the implementation is accepted; fold the board requirements into `board-data` and `herdr-board`
