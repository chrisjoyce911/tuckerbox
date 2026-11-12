# Model

## Skipped

No new concurrent behaviour: the tick is one more commit pushed under the
existing claim, exactly like a fix push, and the merge stays pinned to a
green head, which `formal/ship.tla` already checks. The archived-tasks read
happens under the change lock, a single writer (`formal/runs.tla`).
