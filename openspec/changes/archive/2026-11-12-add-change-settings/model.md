# Model

## Skipped

Single writer under an existing lock: `tuckerbox set` writes the change's sidecar while holding the change lock that runs, `edit`, `tasks` and `approve` already take (`formal/runs.tla`, `OneRunPerChange`). A run reads the settings only once it holds that lock, as the run gate already does. Nothing is shared between machines except through git commits.
