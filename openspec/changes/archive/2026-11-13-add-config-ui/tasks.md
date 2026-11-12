# Tasks

## 1. Key table and sources (repo-config, config-cli)

- [x] 1.1 Gather every key the loader knows into one table (path, type, allowed values, scope, default, secret); make `check`'s repository-only and machine-only rules read it; a unit test fails when the loader accepts a key the table lacks
- [x] 1.2 Test "Scopes agree" (for every `machine` and `repo` row, `check` refuses it in the other file)
- [x] 1.3 Track each effective value's source (`repo`, `machine`, `default`) and the machine value it overrode in the merged config
- [x] 1.4 `tuckerbox config show` and `--json` (key, value, source, scope, file, allowed values; each file's path and revision), machine-scope keys under "this machine only" with the machine's name; document in README
- [x] 1.5 Test "Repository value wins"
- [x] 1.6 Test "Built-in default"
- [x] 1.7 Test "Permission profile"
- [x] 1.8 Test "Revision for a later save" (revision is the SHA-256 of the file's bytes; none when absent)
- [x] 1.9 `tuckerbox config get <key>` for a key or a table, with `--json` and a nearest-key suggestion
- [x] 1.10 Test "Step setting"
- [x] 1.11 Test "Misspelt key"

## 2. Credentials file (repo-config, config-cli)

- [x] 2.1 Read `credentials.toml` from the machine config folder; refuse it when group or others can read it (macOS, Linux); put its token after `token_env` and before the default variables in forge access; `doctor` names the source; refuse `token` in machine `config.toml`
- [x] 2.2 Test "Stored token used first"
- [x] 2.3 Test "Token in machine config" (the value is not printed)
- [x] 2.4 Test "File readable by others"
- [x] 2.5 `show` and `get` print secrets only as `set` or `not set` with the source in use
- [x] 2.6 Test "Token set" (the token appears nowhere in text or JSON output)

## 3. Saving (config-save, config-cli)

- [x] 3.1 Line-level TOML rewrite for set and unset (key line, new key at table end, new table at file end, whole multi-line arrays, empty `[steps.<id>]` removed); write by temporary file and rename
- [x] 3.2 Test "Comment kept"
- [x] 3.3 Check before saving: parse, merge with the other file, run the config checks, compare problems before and after; list problems a save causes in active changes without blocking
- [x] 3.4 Test "Removing a step another step needs"
- [x] 3.5 Test "Fixing one of two problems"
- [x] 3.6 Test "Narrowing the allow-list"
- [x] 3.7 Per-file OS lock in the machine state folder; `--if-match` compared under the lock; check and write from the file read under the lock
- [x] 3.8 Test "Two people edit at once"
- [x] 3.9 `tuckerbox config set` with several `key=value`, `--unset`, `--machine`, `--dry-run`, `--json`; values parsed by the key's type, lists comma-separated; document in README
- [x] 3.10 Test "Two keys in one save"
- [x] 3.11 Test "Back to the machine default"
- [x] 3.12 Test "Dry run"
- [x] 3.13 Refuse keys in the wrong file, naming the right one
- [x] 3.14 Test "Profile in repository config"
- [x] 3.15 Test "Approval rule on the machine"
- [x] 3.16 Machine saves: create the folder and file if needed; tokens into `credentials.toml` created with mode 0600; never commit
- [x] 3.17 Test "First token on a new machine"
- [x] 3.18 Token prompt without echo, or standard input when not a terminal; refuse a token in the arguments; `--unset` removes it
- [x] 3.19 Test "Token in the arguments" (the value is not printed)
- [x] 3.20 `tuckerbox config edit [--machine]` with the `tuckerbox edit` editor, then the check; no commit
- [x] 3.21 Test "Syntax error after an edit"
- [x] 3.22 Run TLC on `formal/configsave.tla` and its broken variants `blindwrite` and `checkbeforelock` (`/opsx:tlc` locally, `scripts/tla-check.sh` in CI); the model passes and each variant is rejected

## 4. Where a repository save goes (config-save)

- [x] 4.1 Choose the target per save: `--local`, `--pr`, or a pull request when this machine has a forge token for the remote's host, else local; print the target and the reason
- [x] 4.2 Test "No token on this machine"
- [x] 4.3 Test "Pull request demanded but not possible"
- [x] 4.4 Local save into the repository's main checkout (as the board resolves it), no commit, note about committing and existing worktrees
- [x] 4.5 Test "Local save"
- [x] 4.6 Pull request save: fetch base, judge `--if-match` and the check against the base file, temporary worktree on `tuckerbox/config-<UTC time>-<hash>`, one commit, push without force, open a ready merge request through forge access, remove the worktree, print the link; document in README
- [x] 4.7 Test "Pull request" (fake forge; checkout files unchanged)
- [x] 4.8 Test "Base branch moved" (no branch pushed)
- [x] 4.9 Never merge a config pull request in any ship mode
- [x] 4.10 Test "Merge ship mode"

## 5. Settings view (settings-view)

- [x] 5.1 Settings in the board header (window and docked pane, not the drawer), view in place of the columns, Back; reads `tuckerbox config show --json` on the board's machine, dropping stale reads by the board's rule
- [x] 5.2 Node test "Open and go back"
- [x] 5.3 Node test "Space changed during a read"
- [x] 5.4 Repository and "This machine only (<machine>)" sections; editable only where the key's scope allows; value, source badge and the overridden value
- [x] 5.5 Node test "Machine-only key"
- [x] 5.6 Node test "Overridden machine default"
- [x] 5.7 Groups by key prefix (Workflow, Runs, Board, Autopilot, Change overrides, Other; Permission profiles, Forge tokens, Editor, Machines, Other); empty groups hidden; fields by key type
- [x] 5.8 Node test "Workflow group"
- [x] 5.9 Workflow editing: add a step (id and agent), remove, move up or down, as pending edits to `workflow.steps` and `steps.<id>`
- [x] 5.10 Node test "Reorder steps"
- [x] 5.11 Pending edits checked with `config set --dry-run --json`, problems by field, save target and reason shown, Save disabled while an edit adds a problem; one `config set --if-match` per save
- [x] 5.12 Node test "Edit that adds a problem"
- [x] 5.13 Node test "Save as a pull request"
- [x] 5.14 The view only runs `tuckerbox config`; shows a failed command's message and keeps pending edits
- [x] 5.15 Node test "Command refused"
- [x] 5.16 On "changed since read": reload, keep pending edits, mark keys changed meanwhile
- [x] 5.17 Node test "Someone saved first"
- [x] 5.18 Forge tokens group: `set` or `not set` with source; Set token opens a pane running the prompt on the board's machine; reload after it closes
- [x] 5.19 Node test "Set a token" (no token value in the mod's commands or state)
- [x] 5.20 Broken file: show the error with file and line, no fields for that file, Edit file opens a pane running `tuckerbox config edit` (`--machine` for machine config)
- [x] 5.21 Node test "Syntax error in repository config"
- [x] 5.22 Document the Settings view in README
