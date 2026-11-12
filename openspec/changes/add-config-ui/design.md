# Design

## Context

See proposal.md for why. What exists, or is planned in approved changes:
- `add-core-runner` (archived, `repo-config`): `.tuckerbox/config.toml` in the repository, machine `config.toml` in the user's config folder, repository values win, permission profiles and `editor` are machine-only, `tuckerbox check` names every problem with file and key. Each run reads the config in the folder it runs in, which for a run with a worktree is the worktree's own committed copy.
- `add-herdr-board`: `require_approval` (repository only), `[board] columns` (either), the board mod with its header, panes and "Actions go through Tuckerbox" rule.
- `fix-board-safety`: the board reads the repository's main checkout, never a change worktree, and drops stale reads (`formal/boardview.tla`).
- `add-change-settings`: `[overrides]` (repository only), per-change settings in the change sidecar, sources `default`, `machine`, `repo`, `change`, and `tuckerbox set` for one change.
- `add-forge-shipping`: `ship`, `[shipping]`, `[forge]` (repository only); `machine` and `[forges."<host>"]` with `token_env` and `borrow_cli_token` (machine only); forge access over HTTP from Go; tokens never printed.
- `add-operating-modes`: `mode`, `[fleet]`, `[team]` (repository only); `[machines.<name>]`, `fleet_capacity` (machine only).
- `add-autopilot`: `[autopilot] scope` and `interval` (either).

## Goals / Non-Goals

**Goals:**
- One place, in the terminal and on the board, to see every setting, its value and why it has that value.
- Changing a setting can't produce a config `tuckerbox check` rejects, and can't silently undo someone else's change.
- Repository config changes are reviewed like code. Machine config and tokens never leave the machine.

**Non-Goals:**
- **Per-change settings.** `add-change-settings` owns them, with pickers on each card. The Settings view links to nothing per change, and `tuckerbox config` does not read sidecars except to report problems a save causes.
- **New config keys.** Apart from the credentials file, this change adds no setting. It edits what other changes define.
- **Copying machine config between machines.** Each machine's owner sets it there, as `add-operating-modes` decided for sign-in.
- **An OS keychain.** See "Where tokens are stored".
- **Tracking config pull requests** after they are opened. The save prints the link; the forge does the rest.
- **Editing role prompts.** They are files, not keys; `tuckerbox config edit` or the editor covers them.

## Decisions

### One key table

`check` already knows, per key, whether a repository may set it. Today that knowledge is spread over the loader (`internal/config/load.go`). This change gathers it into one table: for each key, its path (`steps.<id>.effort`), its type, its allowed values when fixed, its scope (`repo`, `machine`, `either`), its built-in default and whether it is a secret. `check`, `config show` and the view all read it.

Why: the Settings view and `config set` must never disagree with `check` about where a key belongs. With one table, a later change that adds a key adds one row, and the key shows up everywhere. Patterned keys (`steps.<id>`, `agents.<name>`, `permissions.<name>`, `machines.<name>`, `forges."<host>"`) are rows with a placeholder.

Alternative considered: a hand-written list in the board mod. Rejected: two lists drift, and the mod would have to learn every new key.

### Sources

The loader already merges machine and repository values. It now also keeps, per key, which file the value came from, or `default`, and the machine value it overrode. The source names match `add-change-settings` (`default`, `machine`, `repo`), so a card and the Settings view say the same thing. `change` never appears here: that is per change.

### Command shape

```
tuckerbox config show [--json]
tuckerbox config get <key> [--json]
tuckerbox config set [--machine] [--local|--pr] [--if-match <rev>] [--dry-run] [--json] <key>=<value>... [--unset <key>]...
tuckerbox config edit [--machine]
```

`config set` takes several keys because the view saves a pending list as one save, and one save is one commit or one pull request. `tuckerbox set` (change settings) stays one key per call: it writes a different file, and approval rules depend on each key.

Keys and values use dotted paths and plain strings, as `tuckerbox set` does. A list is comma-separated (`workflow.steps=plan,build,review`). A quoted host keeps its dots: `forges."git.example.org".token`.

### Checked before saving

A save edits the file's text in memory, parses the result, merges it with the other file and runs the config checks. It compares the problems before and after: a save that adds a problem is refused; problems the file already had are listed but don't block, so a person can fix a broken file one key at a time.

Problems in active changes (a change override that a narrowed `[overrides]` no longer allows, a `depends_on` that names a removed change) don't block. The change is what needs fixing, and blocking would make narrowing the allow-list impossible, which `add-change-settings` relies on for rollback. The save lists them.

### Keeping the file's shape

The same line-level rewrite `approve` and `tuckerbox set` use: change the key's line, add it at the end of its table, or add the table at the end of the file. Comments and order stay. A multi-line array is replaced as a whole. Removing the last key of a `[steps.<id>]` table removes the table. Then write a temporary file in the same folder and rename it over the old one, so a run starting at that moment reads the old file or the new one, never half.

### No lost saves (`formal/configsave.tla`)

A file's revision is the SHA-256 of its bytes, given by `config show --json`. `config set --if-match <rev>`:
1. Takes the file's lock: an OS file lock in the machine state folder, named from the file's path, as the run lock is.
2. Re-reads the file and compares its revision. Different: refuse with "changed since read".
3. Applies the edit to what it just read, runs the check on the result, writes.
4. Releases the lock.

Without `--if-match`, `set` reads the file under the lock and goes on from step 3, so two terminals are still serialised. The model's broken variants show both halves matter: without the compare an older read overwrites a newer save, and checking before the lock lets two edits that are fine alone combine into a broken file.

A person editing the file in their editor holds no lock. Their editor may overwrite a save, as it would any change on disk. The view's next read shows what is there.

### Where a repository save goes

The repository's config is committed. A save that only writes the checkout changes nothing for other machines or for runs that make their worktree from the remote, until someone commits and pushes. So the default is a pull request when one can be opened:

- **`--pr`, or the default when this machine has a token for the remote's forge** (forge access from `add-forge-shipping`): fetch the base branch; judge `--if-match` and the check against the base branch's file; make a temporary worktree from the fetched base on a new branch `tuckerbox/config-<UTC time>-<short hash of the edit>`; write, commit (message lists the keys), push without force; open a merge request ready for review titled `Configure Tuckerbox: <keys>`; remove the worktree. The checkout is not touched. The push follows `forge-shipping`'s safety rules: never the base branch, never forced.
- **`--local`, or the default without a token:** write the main checkout's file (the same checkout `fix-board-safety` resolves for the board), don't commit, and say so. The output says runs in existing worktrees keep their copy.

The default is decided per save and printed, and the view shows it before Save, with the reason ("no token for github.com on this machine").

Tuckerbox never merges a config pull request, even with `ship = "merge"`. That mode merges changes it built under an approval; a config change has no approval, and it changes the rules every later run follows. In this repository, `AGENTS.md`'s review rule applies to it like any pull request.

A config pull request needs no OpenSpec change. It is a settings change, reviewed in the pull request.

Alternative considered: always save locally and let the person commit. Rejected as the default: it is the easiest way to end up with machines that disagree. Kept as `--local`, and as the fallback the owner asked for while forge access is not set up.

### Machine saves

`--machine` writes the machine `config.toml`, creating the folder and file if needed. It is never committed, pushed or shown to other machines. The view's machine section is the board's machine: on a board pinned to another machine, it runs `tuckerbox config` there (through the board's existing way of running commands on a card's machine) and says which machine it is.

### Where tokens are stored

`add-forge-shipping` reads tokens from environment variables named in machine config, the default variables, or `gh`/`glab`. None of these can be set from a UI. The owner asked for write-only tokens, so this change adds a store:

- `credentials.toml` beside machine `config.toml`, with `[forges."<host>"] token = "..."`. Separate from `config.toml` so the config can be shown, copied or pasted into an issue without leaking a token.
- Created with mode 0600 on macOS and Linux; refused if group or others can read it, as SSH does with keys. On Windows the user's `%AppData%` already limits it to the user.
- In the token order, after `token_env` (an explicit choice for that host) and before the default variables (a stored token is also an explicit choice, made for Tuckerbox).
- Entered at a prompt that does not echo, or on standard input from a script. Never as an argument: arguments show in process lists and shell history.

Alternative considered: the OS keychain (macOS Keychain, Windows Credential Manager, Secret Service). Better at rest, but each needs a different tool or cgo, and Linux servers often have no Secret Service. See Open Questions.

### Settings view

The view replaces the columns in the board window or docked pane, as card detail does. It is not in the drawer, which is a glance, not an editor.

- It reads `tuckerbox config show --json` and drops stale reads by the same rule as the board (`fix-board-safety`), so no new model is needed for the reads.
- Repository and "This machine only (<machine>)" sections. Groups are a fixed list in the mod mapped to key prefixes (Workflow: `workflow`, `steps`; Runs: `max_parallel`, `ship`, `require_approval`, `shipping`, `mode`, `fleet`, `team`; Board: `board`; Autopilot: `autopilot`; Change overrides: `overrides`; machine side: `permissions`, `forges`, `editor`, `machines`, `fleet_capacity`, `machine`). Any key no group claims goes to Other, so a new key is never hidden. A group with no keys is hidden.
- Fields use the key table's type: a picker for fixed values, a number field, a list editor, text otherwise. Agent pickers list `agents` from the effective config plus the built-in agents.
- Pending edits are checked with `config set --dry-run --json` as they change, debounced, and the result is shown by each field.
- Save runs one `config set --if-match <rev> <edits>`. On "changed since read" the view reloads and marks the pending keys whose value changed underneath.
- Tokens: Set token opens a pane running the prompt. The token never passes through the mod, its arguments or its memory.
- A file that doesn't parse shows the error and an Edit file button (a pane running `tuckerbox config edit`), as card detail's Edit does for change files.

### How this fits the unarchived changes

As `add-change-settings` and `add-forge-shipping` do, this change writes no deltas against capabilities still in active changes:
- `Stored forge tokens` adds to `forge-access`'s token order.
- `settings-view` adds to `herdr-board` (the header gains Settings).
- `Every key has one scope` covers the repository-only and machine-only rules those changes add to `repo-config`; it does not change any of them.

Folding these in is a tidy-up after the changes are archived.

### Model

`formal/configsave.tla` (see model.md): several writers save the same file. The Settings view's reads reuse `formal/boardview.tla`'s rule.

## Risks / Trade-offs

- **[Two config pull requests that clash]** → Each passes the check alone; merged together they could fail it (one removes a step, the other points `on_reject` at it). Git may merge them without a conflict. Mitigation: the repository's CI can run `tuckerbox check`; this repository's CI runs the test suite, which loads its own config. See Open Questions.
- **[Saving to the checkout when someone has uncommitted edits there]** → The revision covers the file on disk, uncommitted edits included, so the view sees them and the save keeps them. The output says nothing was committed.
- **[A token in the credentials file at rest]** → Plain text, readable only by the user, the same as `gh`'s own store on most systems. Never printed, logged, or given to agents (`forge-access` rules).
- **[Editors that don't take the lock]** → A person's editor may overwrite a save made while the file was open. The view's next read shows the file as it is.
- **[A running run doesn't see a change]** → A run uses the config it loaded. A new worktree is made from the base branch, so a local save reaches new worktrees only once committed and pushed. Both messages say so.
- **[Key table drift]** → `check` and `config` read the same table, and a test fails when the loader accepts a key the table lacks.

## Migration Plan

- Nothing changes for anyone who edits TOML by hand. Existing files keep working; `config set` keeps their comments.
- No token store exists until a person sets a token; then `credentials.toml` is created.
- Rollback: remove the commands and the view. A `credentials.toml` left behind is ignored by older builds; `tuckerbox doctor` says how to move its token to a variable.

## Open Questions

- **Ship before forge shipping?** The change depends on `add-forge-shipping` for pull request saves and token keys. It could instead ship first with local saves only, and gain pull requests and tokens in a follow-up change. That splits the work but not the specs' intent; it would only change `depends_on` and move two task groups.
- **OS keychain later?** The credentials file could later become one of several stores (keychain first, file as fallback) without changing the commands or the view.
- **`tuckerbox check` in CI?** Whether this repository's CI should run `tuckerbox check` on its own config, to catch clashing config pull requests. That is a CI change, not part of this one.
