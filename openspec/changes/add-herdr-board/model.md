# Model

## Skipped

No new shared writer is introduced.
- **History:** each run's history is appended only by that run's own process, the single writer.
- **The board:** only reads, and its actions go through the `tuckerbox` commands whose locking `formal/runs.tla` models.
- **Dependency waits:** a dependent run only reads other runs' states, before it takes a slot. Taking the slot stays the atomic step already modelled.
