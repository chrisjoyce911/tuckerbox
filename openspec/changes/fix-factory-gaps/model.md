# Model

## Model

Closing a run is a new check-then-act race: autopilot reads a run and its change's archive state, and later closes the run, while a person may resume, stop or close it in between and the change may be archived at any time. `formal/autopilot.tla` already models autopilot acting on what it saw under the change lock, so closing is added there.

Priority publishing uses the approvals ref's publish routine (fetch, one commit, push with `--force-with-lease`, retry), already modelled by `formal/approvals.tla` (`NoLostApproval`); a refused move is re-planned on the fetched order, which is the model's "apply the one change to the new version". Conflict rounds push one more commit with the lease, which `formal/ship.tla` already covers. Neither model changes.

### Files

| File | Role |
|---|---|
| `formal/autopilot.tla`, `formal/autopilot.cfg` | Model, now with `archived`, `ArchiveElsewhere`, `PersonClose`, `AutoClose`; TLC must pass |
| `formal/autopilot_broken_closeunseen.tla`, `.cfg` | Broken variant: autopilot closes on what it saw, without checking the run under the change lock; TLC must reject (`ClosesOnlyIdleArchived`) |

The existing broken variants are unchanged copies of the earlier model and are still rejected.

### Actors and shared state

- **Someone else:** archives the change on the default branch at any time (`ArchiveElsewhere`), even while its run is active. Archiving is never undone.
- **A person:** may also close a run with no agent (`PersonClose`), and may start a new run of a closed change that is not archived.
- **Autopilot:** reads `archived` with the run (`Look`), and closes a `ready`, `blocked`, `stopped` or crashed run of an archived change (`AutoClose`) after checking under the change lock that the run is still the attempt it saw. It never starts or resumes a run of an archived change.
- **Ghost:** `badClose`, set if autopilot closes a run that has an agent or whose change isn't archived.

### Invariants

| Invariant | Meaning | Named by requirement |
|---|---|---|
| `ClosesOnlyIdleArchived` | Autopilot closes only a run with no agent whose change is archived | run-closing: Runs of changes merged elsewhere |
| `ApprovedAtStart` | Now also: never starts a run of an archived change | run-closing: Closed runs stay closed |
| `PersonStopsHold` | Now also: never resumes a run of an archived change | run-closing: Closed runs stay closed |
| `ActsOnSeenAttempt`, `CrashBudget`, `TypeOK` | As before, including `AutoClose` | autopilot |

### Bounds

Unchanged: `Changes = {c1, c2}`, `Groups = 2`, `MaxAutoResumes = 1`, `MaxAttempt = 4`. TLC (tla2tools 1.7.4) explores 895 108 distinct states in a few seconds. `autopilot_broken_closeunseen` is rejected after 7 states: autopilot sees a blocked run of an archived change, the person resumes it, and autopilot closes the now active run.
