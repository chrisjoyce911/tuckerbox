# Tasks

## 1. CI tasks (task-scope)

- [x] 1.1 `scope.IsCI`, `Task.CI`, `UntickedBeforeShip`, `WithoutCI` and `TickCI`; verify with `TestCITasks` in `internal/scope`
- [x] 1.2 The ship gate in `internal/runs` leaves CI tasks out of the scope and completeness checks when shipping to a forge, and keeps them in local mode; verify with `TestCITaskBlocksInLocalMode` and `TestCITaskDoesNotHoldBackShipping`
- [x] 1.3 Shipping ticks CI tasks once CI is green, commits `Tick <change> task N.M`, pushes and waits for CI again; nothing is ticked when CI fails; verify with `TestCITasksTickedOnceCIIsGreen` and `TestCITasksNotTickedWhenCIFails` in `internal/ship`
- [x] 1.4 Step brief says not to tick CI tasks only when shipping to a forge (verify with `TestBriefCITasksOnlyWhenShippingToAForge` and `TestCIBriefFollowsTheShipMode`); README and `openspec/config.yaml` document `(ci)`

## 2. Archived on the branch (task-scope)

- [x] 2.1 Read the archived `tasks.md` when the change's folder is gone and it is in the worktree's archive; count the change complete; verify with `TestArchivedInBranchIsComplete`
- [x] 2.2 Shipping doesn't archive an already archived change again; verify with `TestArchivedInBranchShipsWithoutArchivingAgain` and `TestArchivedOnBranchIsNotArchivedAgain`

## 3. Integration

- [x] 3.1 (ci) `go vet`, `go test -race` and the TLA+ checks pass on Linux, macOS and Windows CI
