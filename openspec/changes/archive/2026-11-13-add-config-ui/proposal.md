# Proposal

## Why

The owner asked: "Do we have a proposal, or do we need a new proposal that
allows us to bring up an interface for the configuration of Tuckerbox?"

We need a new one. No active change covers it:
- `add-change-settings` lets one change pick its own agent, model, effort
  and profile, with pickers on its card. It does not edit repository or
  machine config.
- `add-herdr-board`, `add-forge-shipping`, `add-operating-modes` and
  `add-autopilot` each add config keys, but the only way to set any of them
  is to edit TOML by hand and run `tuckerbox check`.

Today a person has to know every key, which file it belongs in, and which
value wins. A wrong file is caught only by `check`, after the edit. And a
repository's config is committed, so changing it is a code change that
ought to be reviewed like one.

## What Changes

- **`tuckerbox config show|get|set|edit`**, all with `--json`:
  - `show` lists every key with its effective value, its source (`repo`,
    `machine` or `default`) and its scope (`repo`, `machine` or `either`),
    plus each file's revision. Machine-only keys are in a separate section
    marked "this machine only".
  - `get <key>` reads one key or table.
  - `set <key>=<value>...` and `--unset <key>` change one or more keys in one
    save. `--machine` writes machine config. `--dry-run` only checks.
  - `edit [--machine]` opens the file in the editor and checks it after.
- **Every save is checked first.** The new file is merged with the other
  one and run through every check `tuckerbox check` does. A save that adds
  a problem writes nothing.
- **No lost saves.** `set --if-match <revision>` refuses if the file changed
  since it was read. Saves take a lock and write by rename. TLA+ model
  `formal/configsave.tla`, with two broken variants TLC rejects.
- **Repository config is committed, so saves default to a pull request.**
  When this machine has a token for the forge, a save goes on a new branch
  as a merge request ready for review, through `add-forge-shipping`'s forge
  access. Otherwise it writes the checkout's file and says it is not
  committed. `--pr` and `--local` choose explicitly. Tuckerbox never merges
  a config pull request.
- **Tokens are write-only.** A forge token can be entered with
  `config set --machine forges.<host>.token` at a prompt that does not echo.
  It is stored in a new `credentials.toml`, readable only by the user, and
  shown only as `set` or `not set` with its source.
- **A Settings view in the Herdr board,** opened from the board header. It
  shows a Repository section and a "This machine only" section, each value
  with a source badge, grouped as Workflow, Runs, Board, Autopilot, Change
  overrides, Permission profiles, Forge tokens, Editor and Machines. Edits
  collect as pending and are saved together through `tuckerbox config set`.
  The view writes no files itself.
- **One scope table.** Each key's scope comes from one table that `check`,
  `config` and the view share. A repository can still never set a
  machine-only key.

## Capabilities

### New Capabilities

- `config-cli`: the `tuckerbox config` commands, the key catalogue,
  sources, scopes, and how secrets are hidden.
- `config-save`: checking before saving, `--if-match`, atomic writes, and
  where a save goes (checkout, pull request or machine).
- `settings-view`: the Settings view in the board mod.

### Modified Capabilities

- `repo-config`: one scope table for every key, the credentials file and
  its place in the token order, and its file mode.

`forge-access` (in `add-forge-shipping`, not archived) gains the stored
token in its token order. `repo-config` keeps that rule and says it adds to
`forge-access` (see design.md).

## Impact

- Depends on `add-herdr-board` (the board mod, its header and panes),
  `fix-board-safety` (the stale-read rule the view reuses),
  `add-change-settings` (`[overrides]`, which the view edits) and
  `add-forge-shipping` (forge access for pull request saves, and the token
  keys). Keys from `add-operating-modes` and `add-autopilot` appear through
  the shared key table when those are built, with no extra work.
- New commands under `tuckerbox config`. The config package gains a key
  table (scope, type, allowed values, secret or not) and source tracking.
- A new machine file, `credentials.toml`. No repository file changes shape.
- The board mod gains the Settings view.
- No change for anyone who keeps editing TOML by hand.
