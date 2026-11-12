# Proposal

## Why

Every change in a repository runs the same workflow with the same agents.
That is the right default, but not always the right choice. A small doc fix
does not need the strongest model at high effort. A risky refactor may want
Codex to build and Claude to review, or the other way round. Today the only
way to get that is to edit the repository's `.tuckerbox/config.toml`, which
changes every change at once.

Factory lets a repository's workflow file override a base one. We want the
same layering one level further down: a change can override how its own
steps run, inside limits the repository sets, without ever granting itself
more permission than the machine allows.

## What Changes

- **Per-change step settings** in `openspec/changes/<id>/tuckerbox.toml`,
  the sidecar that already holds `title`, `depends_on` and `[approval]`:
  - `[steps.<step>]` with `agent`, `model`, `effort` and `permissions`
    (a permission profile name).
  - `machines = ["studio", "mini"]`: the machines the change prefers. It
    has no effect in solo mode. `add-operating-modes` uses it for placement.
- **Repository allow-list:** `.tuckerbox/config.toml` gains `[overrides]`
  listing the agents, models per agent, efforts and permission profile
  names a change may choose. Nothing in the list means a change may
  override nothing. `[overrides]` is a repository rule; machine config may
  not set it.
- **Machine config still owns grants.** A change can only name a profile.
  The profile's flags, and which remotes may use it, still come from
  machine config alone.
- **Effective settings:** each step runs with built-in default, then
  machine, then repository step, then change override. Tuckerbox records
  the settings each step started with, and where each value came from.
- **`tuckerbox set <change> step.<step>.<key>=<value>`** and
  `tuckerbox set <change> --unset step.<step>.<key>` (also `machines=...`)
  write the sidecar. Like `approve`, they check the value, take the change
  lock, keep other keys and comments, and never commit.
- **Approval:**
  - Changing a step's `agent` or `permissions` voids approval.
  - Changing `model`, `effort` or `machines` keeps it.
  - Approval now holds two hashes: the text, as today, and the settings
    that void it. `resume` and `rerun` check the settings hash, so a
    stopped run can't pick up an unapproved agent.
- **Board:** each card shows every step's effective agent, model, effort
  and profile, and where each came from. Card detail has pickers that run
  `tuckerbox set`, offering only allowed values. A picker that would void
  approval says so first.

## Capabilities

### New Capabilities

- `change-settings`: the per-change settings file, the allow-list check,
  how effective settings are worked out, `tuckerbox set`, and how settings
  affect approval and runs.
- `settings-board`: effective settings on board cards and the pickers in
  card detail. It adds to `board-data` and `herdr-board` once
  `add-herdr-board` is archived.

### Modified Capabilities

- `repo-config`: `[overrides]` is repository-only, and a change override
  never carries permission flags.

`change-approval` (in `add-herdr-board`, not archived yet) gains the
settings hash. This change keeps that rule in `change-settings` and says it
narrows `Approval hash` (see design.md).

## Impact

- Depends on `add-herdr-board` (approval, the sidecar, `board --json`, the
  board mod). Must be archived after it.
- Config: new repository table `[overrides]`. New sidecar keys
  `[steps.<step>]` and `machines`, and `settings_hash` in `[approval]`.
- Run state and history: each step's effective settings and their sources.
- The board mod gains a settings section and pickers.
- No change for repositories without `[overrides]` or changes without
  overrides. Existing approvals stay valid: a missing `settings_hash` is
  read as the hash of no overrides.
