# Model

## Model

Priority adds a writer to the queue's order: `tuckerbox priority` changes
ranks while runs wait for a slot. A run that chooses the first run and a
run that takes a slot are no longer the same moment unless the design
makes them one. `formal/runs.tla` doesn't help here. Its `TakeSlot` lets
any queued run take a free slot, so it accepts every order, the wrong ones
included. So the ordering is modelled on its own.

### Files

| File | Role |
|---|---|
| `formal/priority.tla`, `formal/priority.cfg` | Model; TLC must pass |
| `formal/priority_broken_join.tla`, `.cfg` | Broken variant: a run publishes its ticket without the sidecar lock, so it can land inside another run's admission check; TLC must reject (`OrderedAdmission`) |
| `formal/priority_broken_decide.tla`, `.cfg` | Broken variant: the admission check takes no lock and the slot is taken in a later step, on that decision; TLC must reject (`OrderedAdmission`) |
| `formal/priority_broken_halfmove.tla`, `.cfg` | Broken variant: the check and the slot try are one step, but without the sidecar lock, so a move can be half written; TLC must reject (`OrderedAdmission`) |

### Actors and shared state

- **Runs (`Runs = 1..NRuns`, numbered in request order):** `Join` (publish
  a ticket, under the lock), `Check` (take the lock, list the queue, this
  run is first), `TakeSlot` (still under the lock: take a free slot, or
  go back to waiting; then let the lock go), `Finish` (end, freeing the
  slot without the lock).
- **`tuckerbox priority`:** `BeginMove` takes the sidecar lock and picks
  new ranks. `WriteOne` writes one change's file. `EndMove` lets the lock
  go once every file is written.
- **Shared state:** `prio` (the ranks the files hold now), `committed` (the
  ranks after the last finished move), `slots`, `lock` and `holder`.
- **Atomic:** each action above. A ticket write and a single file write
  are each one step.
- **Not atomic:** a move (several writes), and an admission (a check, then
  a slot try). A run can end, and free its slot, between the two.
- Ranks are admission ranks, with dependencies already folded in.
  Dependency waits happen before a run joins the queue, as `runs.tla`
  and `add-herdr-board` say, so they are not part of this model.

### Invariants

| Invariant | Meaning | Named by requirement |
|---|---|---|
| `OrderedAdmission` | A run takes a slot only when, at that moment, it is first among the queued runs by the ranks of the last finished move (lower rank, then earlier request) | change-priority: Queue follows the order |
| `WithinLimit` | At most `MaxParallel` runs hold slots | change-runs: Parallel limit |
| `TypeOK` | Variables keep their types | (type check) |

### Bounds

`NRuns = 3`, `MaxParallel = 1`, `Ranks = {1, 2}`.
- Three runs give a run holding the slot, a run checking, and a run that
  arrives or is moved ahead of it.
- One slot makes every admission a choice.
- Two ranks allow ties (request order decides) and overtaking (rank
  decides), and a move that changes two files.
- `Finished` lets TLC stop once every run is done, so the bound is not
  reported as a deadlock.

TLC (tla2tools 1.7.4, `scripts/tla-check.sh`) explores 11 936 distinct
states in about a second. Each broken variant is rejected within 6 states:
- `priority_broken_join`: run 1 joins and checks (it is first), run 2,
  ranked above it, publishes its ticket during the check, and run 1 takes
  the slot. With a run holding the slot, the same happens when that run
  ends between the check and the slot try, the case the design must
  survive.
- `priority_broken_decide`: run 1 decides it is first without the lock,
  run 2 (a better rank) joins, and run 1 takes the slot.
- `priority_broken_halfmove`: a move has written its first file, and a run
  is admitted ahead of one that ties with it on the finished ranks and was
  requested first.
