# Tasks

## 1. Allow-list and sidecar (repo-config, change-settings)

- [x] 1.1 Parse `[overrides]` (agents, models per agent, efforts, permissions) from repository config only; refuse it in machine config; `tuckerbox init` comments show an example; document it in README
- [x] 1.2 Test "Machine config sets the allow-list"
- [x] 1.3 Test "Allowed profile not defined on the machine"
- [x] 1.4 Parse `[steps.<step>]` (agent, model, effort, permissions) and `machines` from the change sidecar, keeping `title`, `depends_on` and `[approval]`; refuse other keys in step tables and steps not in `[steps]`
- [x] 1.5 Test "Override one step"
- [x] 1.6 Test "Unknown step"
- [x] 1.7 Test "Raw flags in an override" (the value is not printed)
- [x] 1.8 Test "Machines in solo mode"
- [x] 1.9 Check overrides against the allow-list in `tuckerbox check`, naming change, key, value and the allowed values
- [x] 1.10 Test "Value not allowed"
- [x] 1.11 Test "No allow-list"
- [x] 1.12 Test "Model for another agent"

## 2. Effective settings and runs (change-settings)

- [x] 2.1 Work out effective settings per step (change, repo, machine, default) with the source of each; drop an inherited model and effort when the change overrides the agent
- [x] 2.2 Test "Change wins"
- [x] 2.3 Test "Agent changed without a model"
- [x] 2.4 Honour a change's profile name only through machine profiles and `allowed_remotes`, as for repository steps
- [x] 2.5 Test "Profile not granted here"
- [x] 2.6 `run`, `resume` and `rerun` check overrides against the allow-list from the files the run gate judges (worktree, else the pinned commit); refuse before any worktree or run state changes
- [x] 2.7 Test "Allow-list narrowed later"
- [x] 2.8 Record effective settings and sources in `run.json` and the `step_started` history event; never record a profile's flags; document the fields in `internal/history`
- [x] 2.9 Test "History shows the override"

## 3. `tuckerbox set` (change-settings)

- [x] 3.1 `tuckerbox set <change> <key>=<value>` and `--unset <key>` with `--json`, in the worktree else the checkout, under the change lock (`edits.WithChangeLock`), line-level TOML rewrite, no commit, "not committed" note; document it in README
- [x] 3.2 Test "Set an agent" (comments and other keys kept)
- [x] 3.3 Test "Set a value not allowed"
- [x] 3.4 Test "Back to the repository default" (empty table removed)
- [x] 3.5 Test "Set during a run" (lock held by a run process)
- [x] 3.6 Test "Set while blocked"

## 4. Approval (change-settings)

- [x] 4.1 `approve` writes `settings_hash` over sorted `agent` and `permissions` overrides; `unapprove` removes it; approval state is `stale` when either hash is off; a missing `settings_hash` is the hash of no overrides
- [x] 4.2 Test "Agent changed after approval"
- [x] 4.3 Test "Old approval without overrides"
- [x] 4.4 Test "Model changed after approval" (also for `effort` and `machines`)
- [x] 4.5 `resume` and `rerun` refuse on a stale `settings_hash` when approval is required, naming the changed override; a stale text hash alone still lets them run
- [x] 4.6 Test "Agent changed on a stopped run"
- [x] 4.7 Test "Plan step rewrote the design"

## 5. Board (settings-board)

- [x] 5.1 `board --json`: per-step `settings` with sources (recorded values for a running step) and `machines` on every active card; update the board JSON snapshot
- [x] 5.2 Test "Card with an override"
- [x] 5.3 Test "Running step started before an edit"
- [x] 5.4 `board --json --change`: `choices` from the allow-list, with the keys that void approval
- [x] 5.5 Test "Choices"
- [x] 5.6 Board mod: settings section in card detail, pickers with allowed values and "repository default", running `tuckerbox set`; none while queued or running
- [x] 5.7 Node test "Pick a model"
- [x] 5.8 Node test "No pickers while running"
- [x] 5.9 Board mod: confirm before an `agent` or `permissions` pick on an approved card
- [x] 5.10 Node test "Change agent on an approved card"

## 6. Integration

- [ ] 6.1 End to end in a scratch repository: allow Codex, set the build step to Codex, approve, run to `ready`, check history and the card show Codex from `change`
- [ ] 6.2 `go vet ./...`, `go test -race ./...`, `CGO_ENABLED=0 go build ./...` and `scripts/tla-check.sh` pass on Linux, macOS and Windows CI

## Workflow follow-up

- Depends on `add-herdr-board` being archived first.
- Archive this change after the implementation is accepted.
