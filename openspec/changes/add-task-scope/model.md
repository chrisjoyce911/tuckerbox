# Model

## Skipped

No new model is needed. This change adds no writer and no new interleaving.

- **One run per change is unchanged.** A scope is a field of the change's
  one run. Starting the next scope takes the same change lock (`Pick` in
  `formal/runs.tla`). Two scopes of one change can't be active at once,
  because `OneRunPerChange` already rules out two active runs.
- **Rounds still only go up.** A new scope starts a new round, as a rerun
  does, so a late result from the earlier scope names an older round.
  `StaleNeverAccepted` covers it unchanged.
- **The readiness check has one writer.** It reads tasks.md in the worktree
  while the run holds the change lock. `tuckerbox tasks tick`, `edit`,
  `approve` and `unapprove` write only while holding that lock, so nothing
  else changes the file between the step's result and the check.
- **The run gate is unchanged.** The next scope is a start, gated as in
  `add-herdr-board`, which reasons about that race in its own model.md.
- **Dependencies only read.** A dependent run reads its dependency's state
  and tasks.md before it takes a slot, as it reads its state today.
