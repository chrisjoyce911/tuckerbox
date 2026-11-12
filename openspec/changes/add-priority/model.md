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
| `formal/priority_broken_decide.tla`, `.cfg` | Broken variant: a run decides it is first, then takes a slot in a later step, on that decision; TLC must reject (`OrderedAdmission`) |
| `formal/priority_broken_halfmove.tla`, `.cfg` | Broken variant: choosing and taking are one step, but without the sidecar lock, so a move can be half written; TLC must reject (`OrderedAdmission`) |

### Actors and shared state

- **Runs (`Runs = 1..NRuns`, numbered in request order):** `Join` the
  queue, `Admit` (choose the first run and take a slot), `Finish`.
- **`tuckerbox priority`:** `BeginMove` takes the sidecar lock and picks
  new ranks. `WriteOne` writes one change's file. `EndMove` lets the lock
  go once every file is written.
- **Shared state:** `prio` (the ranks the files hold now), `committed` (the
  ranks after the last finished move), `slots`, `lock`.
- **Atomic:** `Admit`, under the sidecar lock, and each single file write.
- **Not atomic:** a move (several writes), and in the broken variants,
  choosing and taking.
- Ranks are admission ranks, with dependencies already folded in.
  Dependency waits happen before a run joins the queue, as `runs.tla`
  and `add-herdr-board` say, so they are not part of this model.

### Invariants

| Invariant | Meaning | Named by requirement |
|---|---|---|
| `OrderedAdmission` | A run takes a slot only when it is first among the queued runs by the ranks of the last finished move (lower rank, then earlier request) | change-priority: Queue follows the order |
| `WithinLimit` | At most `MaxParallel` runs hold slots | change-runs: Parallel limit |
| `TypeOK` | Variables keep their types | (type check) |

### Bounds

`NRuns = 3`, `MaxParallel = 1`, `Ranks = {1, 2}`.
- Three runs give a run that is first, one that overtakes it by rank, and
  one that ties on rank and must lose on request order.
- One slot makes every admission a choice.
- Two ranks allow ties (request order decides) and overtaking (rank
  decides), and a move that changes two files.
- `Finished` lets TLC stop once every run is done, so the bound is not
  reported as a deadlock.

TLC (tla2tools 1.7.4, `scripts/tla-check.sh`) explores 11 664 distinct
states in about a second.
- `priority_broken_decide` is rejected after 5 states: run 1 decides it is
  first while alone, run 2 (a better rank) joins, and run 1 takes the slot.
  A finished move between the decision and the take fails the same way.
- `priority_broken_halfmove` is rejected after 6 states: a move has written
  its first file, and run 3 is admitted ahead of run 1, which ties
  with it on the finished ranks and was requested first.
