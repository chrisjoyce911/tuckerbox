# Model

<!-- Use ONE of the two forms below and delete the other. -->

## Skipped

<!-- Why this change needs no model: name which condition is absent, e.g.
"single writer: only the dispatcher updates the record". -->

## Model

### Files

| File | Role |
|---|---|
| `formal/<name>.tla`, `formal/<name>.cfg` | Model; TLC must pass |
| `formal/<name>_broken_<label>.tla`, `.cfg` | Broken variant: <what was removed or reordered>; TLC must reject |

### Actors and shared state

<!-- Who writes what, and which step is not atomic. -->

### Invariants

| Invariant | Meaning | Named by requirement |
|---|---|---|
| `<InvariantName>` | <what must always hold> | <spec requirement> |

### Bounds

<!-- Constants in the .cfg and why they are large enough to show the bug. -->
