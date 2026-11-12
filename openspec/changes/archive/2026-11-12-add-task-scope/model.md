# Model

## Model

A scope adds one new interleaving: a dependent run reads its dependency's
progress while the dependency's run moves on to its next scope. The
dependency writes under its own change lock. The dependent doesn't hold
that lock, so only what it reads, and how, decides whether it sees a
consistent state.

### Files

| File | Role |
|---|---|
| `formal/scope.tla`, `formal/scope.cfg` | Model; TLC must pass |
| `formal/scope_broken_split.tla`, `.cfg` | Broken variant: the dependent reads A's run state, then A's live tasks.md, in two steps; TLC must reject (`DependentSeesReviewedWork`) |

### Actors and shared state

- **Change A's run** (each step under A's change lock): `Tick` (the
  builder ticks the group in scope), `ReviewDone` (review passes: one
  `run.json` write sets `ready` and records `complete`), `NextScope`
  (`run --tasks next`: `active`, `complete` cleared, in one write).
- **Change B's run** depends on A. `CheckDep` reads A's `run.json` once and
  starts if it is `ready` and `complete`.
- **Shared state:** `aState` and `complete` (A's `run.json`), `ticked` (A's
  tasks.md), `reviewed` (groups whose review passed).
- **Atomic:** each A step (a `run.json` write by rename, under A's lock);
  B's single read of `run.json`.
- **Not atomic, in the broken variant:** B's read of A's state, then of
  A's tasks.md.

### Invariants

| Invariant | Meaning | Named by requirement |
|---|---|---|
| `DependentSeesReviewedWork` | B starts only on an observation in which every group of A had passed review | task-scope: Dependencies wait for the whole change |
| `TypeOK` | Variables keep their types | (type check) |

### Bounds

`Groups = 3`. Two groups are enough for the race: A is ready after group 1,
starts group 2 and ticks it. Three add a second next scope, so a stale
`complete` after more than one scope would also show. `Finished` lets TLC
stop once B has started.

TLC (tla2tools 1.7.4, `scripts/tla-check.sh`) passes the model.
`scope_broken_split` is rejected after 7 states: A ticks and passes group
1 and is ready; B reads ready; A starts group 2 and ticks it; B reads a
fully ticked tasks.md and starts, with group 2 never reviewed.

### What stays covered by `formal/runs.tla`

- **One run per change.** A scope is a field of the change's one run.
  Starting the next scope takes the same change lock (`Pick`), so
  `OneRunPerChange` covers two scopes of one change.
- **Rounds only go up.** A new scope starts a new round, so a late result
  from the earlier scope names an older round (`StaleNeverAccepted`).
- **The readiness check has one writer.** It reads tasks.md in A's own
  worktree while A's run holds the change lock. `tuckerbox tasks`, `edit`,
  `approve` and `unapprove` write only while holding that lock.
