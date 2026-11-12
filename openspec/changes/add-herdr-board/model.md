# Model

## Skipped

No new model is needed. This change adds no shared writer and no new kind of race.

- **History:** each run's history is appended only by that run's own process, the single writer.
- **The board:** it writes no files and no run state. Its actions go through `tuckerbox` commands whose locking `formal/runs.tla` models.
- **Dependency waits:** a dependent run only reads other runs' states, before it takes a slot. Taking the slot stays the atomic step already modelled.

### Editing and ticking against a running change

This is the one place two writers could meet: a person editing or ticking a change's files while a run's agent writes them. We considered a model and decided against it.

- `tuckerbox edit`, `tasks`, `approve` and `unapprove` don't check run state and then write. They take the change's run lock, the same atomic OS file lock that a run process holds while queued or running. Whoever holds it is the only writer.
- In `formal/runs.tla` that lock is `lock[c]`, taken atomically by `Pick`. An edit process is just another member of `Procs` that takes `lock[c]` and never takes a slot. `OneRunPerChange` already says no two processes hold one change's lock at once, so it covers edit-versus-run and edit-versus-edit too.
- The broken form, "read the run state, then write", is the check-then-set race that `formal/runs_broken_lock.tla` already shows TLC rejecting.
- A new model would restate `runs.tla` with relabelled processes. It would add states but no new property.

What the lock can't cover is a person's editor that keeps running after `tuckerbox edit` is killed. That is a process-lifetime question, not an interleaving one. design.md handles it under Risks.

### Approval

Approval is a pure function of file contents: the state is whatever the files hash to. It has no stored flag to keep in step with anything, so a stale approval can't be mistaken for a current one, whatever order edits, pulls and approvals happen in. The run gate reads the files and the hash in one place (the worktree, or one base-branch revision), so there is nothing to interleave.
