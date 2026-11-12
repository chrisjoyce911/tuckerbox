# Model

## Skipped

Single writer under a lock: the fix makes `tuckerbox herdr install` and `uninstall` hold one OS file lock per Herdr home for their whole read, change and record, the same lock pattern `formal/runs.tla` already checks for the change lock. The stale-read fix is inside one mod process with one event loop, with no state shared between processes.
