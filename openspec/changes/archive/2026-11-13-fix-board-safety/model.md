# Model

## Model

The board window and the drawer read `tuckerbox board --json` asynchronously. One event loop runs them, but the person can switch spaces while a read runs, reads for different spaces can overlap, and they can finish in any order. That is a lifecycle advanced by several actors (the person, the poll timer, reads finishing), and PR #25's finding 5 was a race in it, so it is modelled.

The install lock and journal are not modelled: one process works under one OS lock, the pattern `formal/runs.tla` already checks for the change lock, and the journal has a single writer under that lock.

### Files

| File | Role |
|---|---|
| `formal/boardview.tla`, `formal/boardview.cfg` | Model; TLC must pass |
| `formal/boardview_broken_anykey.tla`, `.cfg` | Broken variant: a finished read is applied whatever the current space is, as the old code did; TLC must reject (`ShownIsCurrent`) |

### Actors and shared state

- **The person (`Switch`):** changes the shown space, so its key (folders and machine), any number of times up to the bound, including back to an earlier one (A → B → A).
- **The view (`Start`):** starts a read for the current key at once after a switch, and on its poll timer; at most one read per key runs.
- **Reads (`Complete`):** finish in any order; a result is applied only if its key is still current.
- **State:** `want` (the current key), `shown` (the key whose result is on screen, or none), `inflight` (keys with a read running).
- **Not atomic:** starting a read and its finishing; anything can happen in between.

### Invariants and properties

| Name | Meaning | Named by requirement |
|---|---|---|
| `ShownIsCurrent` | The view never shows a result for a key other than the current one | board-view: Stale reads are dropped |
| `Settles` (temporal) | Once switches stop, the view comes to show the current key's result and keeps it | board-view: Stale reads are dropped |
| `TypeOK` | Variables keep their types | (type check) |

`Settles` needs weak fairness on `Start` and on each `Complete`: a read for the current key does start, and reads do finish. Without fairness on `Start` (the old behaviour, where a running read for A blocked B's refresh) TLC finds a counterexample to `Settles`.

### Bounds

`Keys = {A, B}`, `First = A`, `MaxSwitches = 3`.
- Two keys are enough for overlapping reads and out-of-order completions.
- Three switches allow A → B → A → B, so a read for A can still be running when A is current again and B's read finishes after it.

TLC (tla2tools 1.7.4, `scripts/tla-check.sh`) explores 28 distinct states, with the temporal check, in under a second.
- `boardview_broken_anykey` is rejected after 4 states: a read for A starts, the space switches to B, A's read finishes and A is shown in B.
