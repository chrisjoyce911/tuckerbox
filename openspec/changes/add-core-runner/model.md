# Model

## Model

### Files

| File | Role |
|---|---|
| `formal/runs.tla`, `formal/runs.cfg` | Model; TLC must pass |
| `formal/runs_broken_lock.tla`, `.cfg` | Broken variant: the change lock is checked then set in two steps instead of one atomic file lock; TLC must reject (`OneRunPerChange`) |
| `formal/runs_broken_stale.tla`, `.cfg` | Broken variant: the engine accepts any result in the file regardless of its round; TLC must reject (`StaleNeverAccepted`) |

### Actors and shared state

- **Run processes (`Procs`):** several `tuckerbox` processes on one machine. Each picks a change, takes its change lock, waits for a slot, runs rounds, then releases both.
- **Agents:** write the result file for their change. A late agent from an earlier round may overwrite it (`LateWrite`).
- **Shared state:**
  - `lock`: the per-change file locks, taken atomically
  - `slots`: the `max_parallel` slot locks
  - `result`: the round number recorded in each change's result file
  - `round`: each change's current round, written only by the process holding its lock
- **Not atomic:** an agent writing its result and the engine reading it. The engine must validate the round.

### Invariants

| Invariant | Meaning | Named by requirement |
|---|---|---|
| `OneRunPerChange` | No two processes hold the same change at once | change-runs: Parallel limit |
| `WithinLimit` | At most `MaxParallel` runs are running | change-runs: Parallel limit |
| `StaleNeverAccepted` | The engine never accepts a result from an earlier round | agent-launch: Result file contract |
| `TypeOK` | Variables keep their types | (type check) |

### Bounds

`Procs = {p1, p2, p3}`, `Changes = {c1, c2}`, `MaxParallel = 1`, `MaxRounds = 2`.
- Three processes on two changes is enough for two processes to race for one change while a third waits for the slot.
- Two rounds is enough for a stale round-1 result to sit in the file during round 2.

TLC (tla2tools 1.7.4) explores 496 distinct states in about a second. Both broken variants are rejected within a few hundred states.
