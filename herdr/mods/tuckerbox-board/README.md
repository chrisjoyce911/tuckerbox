# Tuckerbox board (Herdr mod)

A Herdr app mod that shows one repository's OpenSpec changes as a kanban
board. The columns and cards come from `tuckerbox board --json`. The mod
stores no task state, writes no files and never moves a card by itself
(see `openspec/changes/add-herdr-board/specs/herdr-board`).

## Files

| File | What it does |
|---|---|
| `main.js` | Picks the view: the board, or `view: "card"` for one change's detail dialog |
| `board.js` | The board: header (repository, running and needs-you counts), columns, cards |
| `card.js` | Card detail: proposal (markdown), tasks with tick boxes, approval, last validation, dependencies, rounds with findings, history (newest first, local time), actions |
| `model.js` | Data rules with no Herdr imports: reading the document, grouping, counts, times, polling |
| `actions.js` | Which actions each state offers, and `commandLine()`, the one function that builds every tuckerbox command line |
| `perform.js` | Runs an action. Not wired yet (add-herdr-board 6.4 to 6.6): it shows the command it would run |
| `data.js` | Runs `tuckerbox board --json` on the space's machine, or reads the fixtures |
| `ui.js` | Buttons, chips and badges, in theme colors |
| `fixtures/` | `board.json` and `detail.json`: the board-data contract, by example |
| `test/` | `node --test test/` (no dependencies), and `check-fixtures.mjs` |

## Trying it

```sh
herdr-app check herdr/mods/tuckerbox-board      # loads and renders it once
node --test herdr/mods/tuckerbox-board/test/     # action mapping and data rules
node herdr/mods/tuckerbox-board/test/check-fixtures.mjs   # renders the fixtures through herdr-app check
```

**Dev flag:** with `{"fixtures": true}` in a `dev.json` beside `main.js`
(ignored by git), or the prop `fixtures: true` (or a folder), the board and
the detail dialog render `fixtures/board.json` and `fixtures/detail.json`
instead of running `tuckerbox`. The fixtures have the detail of one change,
`add-login-page`; other cards' dialogs say there is no detail for them.

**Polling:** the board reads every 5 s while it is shown and every 30 s while
Herdr keeps it hidden. A window or dialog counts as shown. A pane counts as
shown when its own space and tab are the shown ones and no other pane of that
tab is zoomed. A pane copy knows its pane from Herdr's `pane` prop, else from
the board pane that is focused while it holds the keyboard, else because it
is the only board pane; until then, with several board panes, it counts as
shown while any of them is. The detail dialog reads once when it opens.

**Which repository:** a docked board (`space` prop, or the space of its own
pane) reads its own space: the focused pane's folder only when that pane is
in that space, else a terminal there, else the space's folder, always on that
space's machine. A window follows the shown space. A `folder` prop pins it.

## The board document

`tuckerbox board --json` run in the repository prints the document below.
`tuckerbox board --json --change <id>` prints the same document plus `detail`;
its `cards` must include that change's card (the fixture holds only that one). The mod reads exactly these
fields. Strings may be `""` or missing when they don't apply, numbers `0`,
lists `[]`: the mod treats missing, `null` and empty alike. Times are RFC 3339
with their offset. Fields may be added; renaming or removing one needs a new
`version`, as for `tuckerbox status --json`.

### Document

| Field | Type | Meaning |
|---|---|---|
| `version` | number | Format version, `1` |
| `repo` | string | Normalised remote URL (`github.com/owner/name`), or the checkout path when there is no remote. The key that matches the repository across machines |
| `path` | string | The repository checkout on this machine. Board actions run here |
| `machine` | string | This machine's name |
| `require_approval` | bool | The repository's `require_approval`; `false` means no Approved column and Start on Proposed cards |
| `columns` | list | In board order, see below |
| `cards` | list | Every active change and the 20 most recently archived, see below |
| `detail` | object | Only with `--change`, see below |
| `error` | object | Instead of the rest when the board can't be read: `{ "code", "message" }` |

`error.code` is `not_a_repository` when the folder isn't a Tuckerbox
repository: not inside a git repository, or no `.tuckerbox/config.toml` at
its root (`config.ErrNoRepoConfig`). The board then says so and offers `tuckerbox init` there
(herdr-board: Space outside a repository). Other codes are shown as they are.
The command still exits non-zero with an error document.

### Column

| Field | Type | Meaning |
|---|---|---|
| `id` | string | `proposed`, `approved`, `queued`, `step:<step>`, `ready`, `done`. Cards name their column by this id |
| `kind` | string | `proposed`, `approved`, `queued`, `step`, `ready` or `done` |
| `step` | string | The workflow step, for `kind: "step"`. Re-run offers these steps, in this order |
| `name` | string | What the board shows: `Proposed`, `Approved`, `Queued`, the step's name capitalised, `Ready`, `Done` |

### Card

| Field | Type | Meaning |
|---|---|---|
| `change` | string | The change id |
| `title` | string | `title` from `tuckerbox.toml`, else the id with dashes as spaces |
| `column` | string | A column `id` |
| `tasks` | object | `{ "done", "total" }` from tasks.md |
| `approval` | object | `{ "state", "by", "at", "committed" }`: `state` is `approved`, `stale` or `none`; `by` the git `user.name`, `at` the time; `committed` is `false` when the approval or a hashed file has uncommitted changes in `edit_folder` |
| `state` | string | The run's state: `queued`, `running`, `blocked`, `stopped`, `failed`, `ready`, or `""` with no run (and for archived changes) |
| `reason` | string | Why it is queued, blocked or failed (`waiting on add-users`, `review limit reached`) |
| `step` | string | The run's current step |
| `agent` | string | The current step's agent (`claude`, `codex`) |
| `round` | number | The run's round, from 1; `0` with no run |
| `since` | string | When the state last changed |
| `branch` | string | The run's branch |
| `worktree` | string | The run's worktree path on `machine`. Open worktree uses it |
| `machine` | string | The machine the run is on (this one, for a card without a run) |
| `edit_folder` | string | Where `tuckerbox edit` would edit: the worktree if there is one, else the checkout. Open proposal reads `<edit_folder>/openspec/changes/<change>/proposal.md` |
| `depends_on` | list | `[{ "change", "column" }]`: each dependency with the column id it is in (`done` when archived, `""` when unknown) |
| `archived` | string | For Done cards: the archive date (`2026-11-08`) |

### Detail (`--change <id>`)

| Field | Type | Meaning |
|---|---|---|
| `change` | string | The change id |
| `proposal` | string | proposal.md's text (markdown) |
| `specs` | list | `[{ "capability", "text" }]`: each spec delta. Edit offers `spec:<capability>` for each |
| `tasks` | list | `[{ "number", "group", "text", "checked" }]` in file order: `number` is `2.1`, `group` the `## ` heading above it (`2. Sign in`) |
| `rounds` | list | `[{ "round", "result", "step", "summary", "findings", "ended" }]`: `result` is the round's last step result (`done`, `rejected`, `blocked`, `failed`) or `""` while it runs; `step` is the step that gave it |
| `history` | list | The run history, oldest first, see below. The board shows it newest first |

A finding is `{ "severity", "file", "detail" }`, as agents write it
(`critical`, `high`, `medium`, `low`, `info`; `file` may be `""`).

A history event has `at`, `event`, `step`, `round`, `agent` and `machine`,
and some of: `result` (step_finished), `to` and `summary` (handoff), `summary`
and `findings` (rejected), `reason` (queued, blocked, failed). `event` is one
of `started`, `queued`, `step_started`, `step_finished`, `handoff`,
`rejected`, `blocked`, `stopped`, `resumed`, `failed`, `ready`.

## Actions

Every button calls `invocation()` in `actions.js`, which calls
`commandLine()`. Until 6.4 to 6.6 wire them, `perform.js` shows the command.

| Action | Offered | Command |
|---|---|---|
| Start | Approved cards; Proposed cards when approval is off | `tuckerbox run <id>` |
| Approve | approval `none` or `stale`, no queued or running run | `tuckerbox approve <id>` |
| Unapprove | approval `approved`, no queued or running run | `tuckerbox unapprove <id>` |
| Stop | queued, running | `tuckerbox stop <id>` |
| Resume | stopped, blocked | `tuckerbox resume <id>` |
| Re-run step | stopped, blocked, failed, ready | `tuckerbox rerun <id> <step>` |
| Follow log | running, blocked, stopped, failed (a Herdr tab) | `tuckerbox logs <id> <step> --follow` |
| Edit | no queued or running run (a Herdr pane) | `tuckerbox edit <id> proposal\|design\|model\|tasks\|spec:<capability>` |
| Validate | after an edit, if the pane's close can't be seen | `tuckerbox validate <id> --json` |
| Tick, Untick | no queued or running run | `tuckerbox tasks <id> tick\|untick <n.m>` |
| Open proposal | the card has an `edit_folder` | Herdr pane, no command |
| Open worktree | the card has a `worktree` | Herdr's `worktree` mod, no command |
| tuckerbox init | the folder isn't a Tuckerbox repository | `tuckerbox init` |

Commands run with `exec` on the card's Herdr machine, in the document's
`path` there, with arguments as a list (no shell). A change id must be
lowercase letters, digits and dashes, and a task number `n.m`, or the
command isn't built. Archived (Done) cards offer nothing.
