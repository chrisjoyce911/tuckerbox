# Proposal

## Why

Every autopilot run on 2026-11-12 stalled on one of two scope gates:

- A change's last integration task is often one only CI can do, such as
  `6.2 Go tests pass on Linux, macOS and Windows CI`. The builder can't
  tick it honestly before shipping, so the run blocked with `scope tasks not
  ticked: 6.2` (seen on add-change-settings) and never reached CI.
- A builder that archived the change on its branch moved `tasks.md` to
  `openspec/changes/archive/<date>-<change>/`, and the scope check then
  blocked with `scope tasks no longer in tasks.md: …` (seen on add-priority).

## What Changes

- **CI tasks.** A task marked `(ci)`, or whose text says it passes (or is
  green) on or in CI, is a CI task. Shipping to a forge (`ship = "mr"` or
  `"merge"`), a CI task doesn't count in the scope check or toward the
  change being complete. Shipping ticks it, in a commit
  `Tick <change> task N.M` pushed to the merge request, once CI is green
  there, and waits for CI on that commit before it merges or reports ready.
  In local mode CI tasks count as they do today. The step brief says not to
  tick them.
- **Changes archived on their branch.** When the change's folder is gone
  from the worktree and the change is in the archive there, the run reads
  the archived `tasks.md`, counts the change as complete and ships it.
  Shipping records the archived folder and doesn't archive it again.
- README and `openspec/config.yaml` document the `(ci)` convention.

## Capabilities

### New Capabilities

### Modified Capabilities
- `task-scope`: adds CI tasks and changes archived on their branch.

## Impact

`internal/scope` (CI tasks, `TickCI`), `internal/runs` (the ship gate reads
archived tasks and leaves CI tasks out when shipping to a forge),
`internal/ship` (ticks CI tasks after green; skips a second archive),
`internal/agent` (brief), README, `openspec/config.yaml`.
