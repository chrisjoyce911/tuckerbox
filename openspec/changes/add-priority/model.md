# Model

## Skipped

No change to `formal/runs.tla` and no new model. We checked whether the new
queue order needs one.

### `runs.tla` never modelled first in, first out
In `runs.tla`, `TakeSlot(p)` lets any process that holds its change lock
take a free slot, in any order. The order is left free on purpose, because
the queue only orders and the slot locks alone bound how many run. So the
model already covers every admission order, priority order included.

The new queue only narrows which waiting run may try for a slot. Every
behaviour it allows is one the model already allows, so `OneRunPerChange`,
`WithinLimit` and `StaleNeverAccepted` still hold. Adding priority to the
model would add states and no property that can fail.

### Ordering is not a safety property here
- **No stall.** The sort key is a total order over the live tickets, so one
  of them is always first. A first ticket can take any free slot. A run
  waiting on dependencies holds no ticket, so it can't be first and stuck.
- **Readers can disagree for a moment.** A waiting run may read priorities
  while `tuckerbox priority` renumbers several files. Two runs may then
  both think they are first, and both take free slots. The slot locks still
  bound the number. Or neither thinks so for one poll, and the next poll
  agrees. Either way the cost is order, not safety.
- **Starvation is intended.** A low rank may wait for ever while higher
  ones arrive. So there is no liveness property to check. FIFO fairness
  wasn't modelled before either.

### Sidecar writes
`priority`, `approve`, `unapprove` and `set` rewrite `tuckerbox.toml` while
holding one OS file lock. That is the atomic-lock pattern `runs.tla` models
as `lock[c]` in `Pick`. Each writer is a process that takes the lock and
never takes a slot. The broken form, reading and writing without the lock,
is the lost update that `formal/runs_broken_lock.tla` already shows TLC
rejecting. The sidecar lock is always taken last and held only for the
write, so it adds no lock-order cycle.
