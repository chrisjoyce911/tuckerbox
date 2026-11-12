# Tuckerbox board (Herdr mod)

A Herdr app mod that shows one repository's OpenSpec changes as a kanban
board. The columns and cards come from `tuckerbox board --json`. The mod
stores no task state, writes no files and never moves a card by itself
(see `openspec/specs/herdr-board`).

## Files

| File | What it does |
|---|---|
| `main.js` | Picks the view: the board, `view: "card"` for one change's detail dialog, `view: "proposal"` (a pane), `view: "drawer"` (the peek drawer) |
| `board.js` | The board: header (repository, running and needs-you counts, Dock or Undock and Close), columns, cards; a running card shows its agent's activity (step, agent and model, `on <n>` with the task's text on hover, time since the last tick, running time, `waiting for you`) |
| `drawer.js` | The peek drawer: nothing without a Tuckerbox repository, else the repository and its running and needs-you counts; a click toggles the board window |
| `keys.json` | The toggle key `tuckerbox herdr install --keys` merges into the person's `keys.json` |
| `card.js` | Card detail: proposal (markdown), tasks with tick boxes, approval, last validation, dependencies, rounds with findings and task scope, activity (every `activity` field and the run's ticks, newest first), history (newest first, local time), step settings with their pickers, actions, and Run group… (where Start is offered, and on Ready cards with unticked tasks) |
| `model.js` | Data rules with no Herdr imports: reading the document, grouping, counts, times, polling |
| `actions.js` | Which actions each state offers, and `commandLine()`, the one function that builds every tuckerbox command line |
| `perform.js` | Runs an action: `exec` of the tuckerbox command on the card's machine (a short-lived `hc run` terminal for a remote card's Start, Run group, Resume and Re-run), or the Herdr tab, pane or view for it. With the fixtures it only says what it would run |
| `edits.js` | Edit tracking: notes the editor's pane, validates when it closes, keeps the result in memory |
| `proposal.js` | `view: "proposal"`: a change's proposal in a pane (Open proposal) |
| `data.js` | Finds the space's Tuckerbox repository, runs `tuckerbox board --json` there on the space's machine and `--repo` on the other machines, or reads the fixtures |
| `ui.js` | Buttons, chips and badges, in theme colors |
| `fixtures/` | `board.json` and `detail.json`: the board-data contract, by example |
| `test/` | `node --test test/` (no dependencies), and `check-fixtures.mjs` |

## Installing

`tuckerbox herdr install` copies this folder (without `test/` and
`package.json`) into the Herdr app's `mods/tuckerbox-board`, after
`herdr-app check` passes on a staged copy, and writes `version.json` there:
the binary's version, commit and a digest of the files. Herdr's home is
`$HERDR_APP_HOME`, else `~/.config/herdr-app`; without it nothing is
written. Two opt-in extras:

- `--keys` merges `keys.json` (the toggle, `mod.open:tuckerbox-board`) into the
  person's `keys.json`: `ctrl-shift-k`, or `cmd-shift-k` on macOS, or
  `--key <key>`. A key bound already in any section but a `mod:` one is
  reported and left alone.
- `--drawer` adds the drawer, `{ "mod": "tuckerbox-board", "props": { "view": "drawer" } }`
  with `id` `tuckerbox-drawer`, to the shown layout's root `floating` list. A
  shipped layout is first cloned to `<name>-tuckerbox` with
  `herdr-app do layout.clone` (Herdr must be running for that).

There is no + menu entry: Herdr documents no way to choose a personal
new-menu. Install records what it changed in `.tuckerbox-install.json` in
Herdr's home, with each file as it was; `tuckerbox herdr uninstall` reverses
exactly that (the old bytes when a file is still as install left it, else
only Tuckerbox's entry) and removes the mod folder. `tuckerbox doctor` says whether the installed copy is the one the
binary ships: the digest in `version.json`, and every shipped file's bytes
(it names a file changed or missing; files Herdr adds, such as
`gpui-kit.d.ts`, don't count).

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

**Which repository:** a `folder` prop (with `machine`, as Dock sets them)
pins it. Otherwise the board takes its space (`space` prop, else its own
pane's, else the shown one for a window or the drawer) and looks, on that
space's machine, at the focused pane's folder when that pane is in the space,
then every other pane there (the shown tab's first), then the space's folder.
The first whose git top level has `.tuckerbox/config.toml` wins; the board
reads at that top level. With none, there is no board: a window opened then
closes with the toast "No Tuckerbox config in this space", the drawer draws
nothing and there is no Dock; a board that already showed one says so.
`git rev-parse --show-toplevel` and the config read are kept 30 s (10 s for
no), so the copies look every second and follow a space switch at once.

**Every machine:** after reading its own repository, the board asks each
other machine `machines()` lists for the same repository with
`tuckerbox board --json --repo <repo>` (8 s each, in parallel). There
`tuckerbox` finds its checkout of that remote through its machine index, so
a machine sees a repository once it has run one of its changes there.
Cards merge by change id: a queued or running copy wins, then any copy with
a run, else the board's own. Each card keeps its machine and checkout, and
its actions and detail run there. A card from a machine with the other
column layout goes in the matching column. A machine with no checkout adds
nothing; one that is offline or fails is named "unreachable" in the header,
with the error as a tooltip, and the other cards still show. A repository
with no remote (its key is its path) is read on its own machine only. The
drawer counts the same merged board.

**Opening, hiding, docking:** `mod.open tuckerbox-board` (the toggle key,
the drawer's click) opens the window or closes it. Dock (window header) runs
`pane.split {direction: "right", pane, mod: "tuckerbox-board", props: {folder, machine}}`
beside the focused terminal of the space, else the shown tab's first, else
`tab.new` with the board as its pane, and closes the window. A docked board's
header has Undock (opens the window, closes the pane) and Close.

## The board document

`tuckerbox board --json` run in the repository prints the document below.
`tuckerbox board --json --change <id>` prints the same document plus `detail`;
its `cards` must include that change's card (the fixture holds only that one). The mod reads exactly these
fields. Strings may be `""` or missing when they don't apply, numbers `0`,
lists `[]`: the mod treats missing, `null` and empty alike. Times are RFC 3339
in the machine’s local zone, with its offset. Fields may be added; renaming or removing one needs a new
`version`, as for `tuckerbox status --json`.

### Document

| Field | Type | Meaning |
|---|---|---|
| `version` | number | Format version, `1` |
| `repo` | string | Normalised remote URL (`github.com/owner/name`), or the checkout path when there is no remote. The key that matches the repository across machines |
| `path` | string | The repository checkout on this machine. Board actions run here |
| `machine` | string | This machine's name |
| `os` | string | The operating system Tuckerbox runs on there (Go's `GOOS`, such as `darwin`, `linux`, `windows`). The answer actions pipe their text in with PowerShell on `windows`, with `sh` elsewhere |
| `require_approval` | bool | The repository's `require_approval`; `false` means no Approved column and Start on Proposed cards |
| `steps` | list of strings | Workflow step ids in order, used for Re-run in either layout |
| `columns` | list | In board order, see below |
| `cards` | list | Every active change and the 20 most recently archived, see below |
| `detail` | object | Only with `--change`, see below |
| `error` | object | Instead of the rest when the board can't be read: `{ "code", "message" }` |

`error.code` is `not_a_repository` when the folder isn't a Tuckerbox
repository: not inside a git repository, or no `.tuckerbox/config.toml` at
its root (`config.ErrNoRepoConfig`). The board looks for the config itself
before it runs `tuckerbox board`, so this is rare; it then says "No Tuckerbox
config in this space" and offers no command (herdr-board: Space without a
Tuckerbox config). Other codes are shown as they are.
The command still exits non-zero with an error document.

Proposed, Approved, In progress, and in the steps layout Queued and each
step column, list their cards in priority order; Ready and Done keep their
order (board-data: Priority on the board). The mod groups cards in the
document's order, so it sorts nothing itself.

Default `[board] columns = "compact"` shows Proposed, Approved (when required),
In progress, Ready, Done. In progress combines queued runs and every workflow
step, including stopped, blocked and failed runs. `columns = "steps"` shows
Queued and each workflow step separately. Repository config overrides the
machine default. `board.json` demonstrates steps; `detail.json` demonstrates compact.

### Column

| Field | Type | Meaning |
|---|---|---|
| `id` | string | `proposed`, `approved`, `in_progress`, `queued`, `step:<step>`, `ready`, `done`. Cards name their column by this id |
| `kind` | string | `proposed`, `approved`, `in_progress`, `queued`, `step`, `ready` or `done` |
| `step` | string | The workflow step, for `kind: "step"`. Legacy documents use these for Re-run; new documents use `steps` |
| `name` | string | What the board shows: `Proposed`, `Approved`, `In progress`, `Queued`, the step's name capitalised, `Ready`, `Done` |

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
| `badge` | string | Current workflow step or `queued` for in-progress cards; empty otherwise. Displayed as a small badge |
| `step` | string | The run's current step |
| `agent` | string | The current step's agent (`claude`, `codex`) |
| `round` | number | The run's round, from 1; `0` with no run |
| `since` | string | When the state last changed |
| `branch` | string | The run's branch |
| `worktree` | string | The run's worktree path on `machine`. Open worktree uses it |
| `machine` | string | The machine the run is on (this one, for a card without a run) |
| `edit_folder` | string | Where `tuckerbox edit` would edit: the worktree if there is one, else the checkout. Open proposal reads `<edit_folder>/openspec/changes/<change>/proposal.md` |
| `depends_on` | list | `[{ "change", "column" }]`: each dependency with the column id it is in (`done` when archived, `""` when unknown) |
| `priority` | number or null | The change's `priority` in the repository checkout on this machine (committed or not), or `null` for none and for archived cards |
| `position` | number | Its place in priority order (priority, then age, then id), from 1. Absent on archived cards. The board shows it as `#n` |
| `archived` | string | For Done cards: the archive date (`2026-11-08`). Their approval is `none`, with blank metadata and `committed: true`; no approval or commit checks are needed |
| `activity` | object | On cards with a running, queued or stopped run: what its agent is doing, see below |
| `settings` | list | Each workflow step's effective settings, in workflow order: `[{ "step", "agent", "model", "effort", "permissions", "started" }]`, each key `{ "value", "source" }` with `source` one of `change`, `repo`, `machine`, `default` and `value` `""` for the agent's own default (or no permission profile). For a step a run is running, the values it started with (from `run.json`), with `started: true`; otherwise what it would run with now. `[]` for archived cards |
| `machines` | list of strings | The change's `machines` from its `tuckerbox.toml`, in order; `[]` for none |
| `error` | string | Optional diagnostic when this card could only be read partially (missing worktree/change folder, malformed metadata, unreadable files or obsolete run step). Other cards remain available. Retained fields may be incomplete; a run with an absent or unknown step stays visible in In progress (compact) or Queued (steps), with badge `queued` |

### Activity (agent-activity)

| Field | Type | Meaning |
|---|---|---|
| `step`, `agent`, `model`, `effort` | string | The run's step, its agent, and the model and effort it runs with (`""` for the agent's default) |
| `round` | number | The run's round |
| `task` | object or null | `{ "number", "text" }`: the first unticked task of the run's scope (or of the change), ship tasks left out, in the worktree's tasks.md. `null` when all are ticked |
| `task_label` | string | `on <n>`, or `all tasks in scope ticked` |
| `last_tick` | object or null | `{ "number", "at", "ticked", "step", "round" }`: the last task ticked in the current run |
| `step_started`, `run_started` | string | When the step and the run started (`""` when unknown, as for a queued run's step) |
| `last_activity` | string | When the agent last did something: the step log's last write (headless), or herdr-core's last output for its terminal |
| `agent_state` | string | `working` or `waiting for you` while the step runs, else `""`. A running card that waits for you counts as needing the person |
| `backend` | string | `terminal` or `headless` |
| `terminal` | object | While a step runs in a terminal: `{ "address", "name", "machine" }`. Watch focuses `address` on the card's Herdr machine |

### Detail (`--change <id>`)

| Field | Type | Meaning |
|---|---|---|
| `change` | string | The change id |
| `proposal` | string | proposal.md's text (markdown) |
| `specs` | list | `[{ "capability", "text" }]`: each spec delta. Edit offers `spec:<capability>` for each |
| `tasks` | list | `[{ "number", "group", "text", "checked" }]` in file order: `number` is `2.1`, `group` the `## ` heading above it (`2. Sign in`) |
| `rounds` | list | `[{ "round", "result", "step", "summary", "findings", "ended" }]`: `result` is the round's last step result (`done`, `rejected`, `blocked`, `failed`, `stopped`) or `""` while it runs; `step` is the step that gave it |
| `history` | list | The run history, oldest first, see below. The board shows it newest first |
| `ticks` | list | The current run's `task_ticked` and `task_unticked` events as `{ "number", "at", "ticked", "step", "round" }`, newest first; the Activity timeline |
| `choices` | object | The settings pickers' allow-list, from `[overrides]` in the repository config where `tuckerbox set` edits the change: `{ "agents", "models", "efforts", "permissions", "voids_approval" }`. `models` maps an agent to its allowed models; `voids_approval` lists the step keys whose change voids an approval (`agent`, `permissions`). Every list is empty when the repository has no `[overrides]` |

A finding is `{ "severity", "file", "detail" }`, as agents write it
(`critical`, `high`, `medium`, `low`, `info`; `file` may be `""`).

A history event has `at`, `event`, `step`, `round`, `agent` and `machine`,
and some of: `result` (step_finished), `to` and `summary` (handoff), `summary`
and `findings` (rejected), `reason` (queued, blocked, failed), `task`
(task_ticked, task_unticked). `event` is one of `started`, `queued`,
`step_started`, `step_finished`, `handoff`, `rejected`, `blocked`,
`stopped`, `resumed`, `rerun`, `failed`, `ready`, `task_ticked`,
`task_unticked`, or a shipping event.
A round is created when work starts or a step result is recorded; queue/admission events alone do not create a round. Stopped (including crashed) rounds have result `stopped` and an `ended` time.

## Actions

Every button calls `invocation()` in `actions.js`, which calls
`commandLine()`; `perform.js` runs it. A command's error is shown as it is
(stderr first); on success its output is shown, so approve's and tick's
"not committed" note reaches the person. After a success every board copy
reads again at once: cards move only when tuckerbox says so.

Tabs and panes open on the shown space's machine when it is the card's
(`tab.new` in that space, `terminal.open` to the right of the focused pane),
else as a new space on the card's machine, since Herdr keeps tabs and splits
on their space's machine.

**Edit and validate:** Herdr's `run()` returns no ids, so the board notes
the panes before opening the editor and claims a new terminal pane as the
editor's only when it is that edit's: on the card's machine (remote pane ids
start with `<machine>/`), in the space it opened in when known, started in
the card's checkout, and claimed by no other edit. When it goes, the board
runs `tuckerbox validate <id> --json` and shows the result on the card and in
its detail, in memory only. If no such pane appears within 10 s, or it could
belong to two edits, or two panes could be it, the board doesn't guess: the
card says it wasn't validated since the edit and the detail offers Validate.

| Action | Offered | Command |
|---|---|---|
| Start | Approved cards; Proposed cards when approval is off | `tuckerbox run <id>` |
| Approve | approval `none` or `stale`, no queued or running run | `tuckerbox approve <id>` |
| Unapprove | approval `approved`, no queued or running run | `tuckerbox unapprove <id>` |
| Stop | queued, running | `tuckerbox stop <id>` |
| Resume | stopped, blocked | `tuckerbox resume <id>` |
| Re-run step | stopped, blocked, failed, ready | `tuckerbox rerun <id> <step>` |
| Follow log | running headless, blocked, stopped, failed (a Herdr tab) | `tuckerbox logs <id> <step> --follow` |
| Watch | running in a terminal (`activity.backend` `terminal` with its address) | Herdr's `agent.focus {terminal}` with `activity.terminal.address`, as `<machine>/<address>` for a card on another machine; Herdr opens the agent's tab from its labels if it isn't showing |
| Edit | no queued or running run (a Herdr pane) | `tuckerbox edit <id> proposal\|design\|model\|tasks\|spec:<capability>` |
| Validate | after an edit, if the pane's close can't be seen | `tuckerbox validate <id> --json` |
| Tick, Untick | no queued or running run | `tuckerbox tasks <id> tick\|untick <n.m>` |
| Open proposal | the card has an `edit_folder` | A pane with this mod's proposal view, read through `tuckerbox board --json --change` |
| Reorder | drag a card within a sorted column (not into another) | `tuckerbox priority <id> <n>` |
| Settings | a step key the allow-list permits, no queued or running run (card detail's Settings) | `tuckerbox set <id> step.<step>.<key>=<value>`; "repository default": `tuckerbox set <id> --unset step.<step>.<key>` |
| Open worktree | the card has a `worktree` | `space.select` of a space already there, else `space.new` in the worktree on the card's machine |

**Settings:** card detail lists each workflow step's agent, model, effort
and permission profile with its source. Clicking a key the allow-list
permits opens its picker: the allowed values (models for the step's
effective agent) and "repository default". A pick runs `tuckerbox set` and
shows its output, including the note that nothing was committed. On an
approved change, a pick of `agent` or `permissions` first says it will void
the approval and runs only on "Change it"; model and effort never ask. No
picker is offered while a run is queued or running.

**Reorder:** a press on a card and a move of more than a few pixels starts a
drag; releasing over a card of the same sorted column drops it above that
card (upper half) or below it (lower half). `dropPosition()` in `model.js`
turns that into the position: above card B, B's position if the card came
from below B, else one less; below the column's last card L, L's position if
it came from above L, else one more. The board then runs
`tuckerbox priority <id> <n>` on its own machine, in its own checkout (even
for a card whose run is on another machine), and shows its error, if any;
the card moves only when the next read shows it. Releasing anywhere but over
the target card (outside it, in another column, off the board), or on the
dragged card itself, moves nothing. Priority and position always come from
the board's own machine: a card merged in from another machine keeps the own
copy's, and a change only another machine has gets none and can't be
dragged. If positions changed between the read and the drop, the card may
land one place off, and the next read shows where it is.

Commands run with `exec` on the card's Herdr machine, in the document's
`path` there, with arguments as a list (no shell). A change id must be
lowercase letters, digits and dashes, a task number `n.m` and a position a
whole number from 1, or the
command isn't built. Archived (Done) cards offer nothing.

Start, Run group, Resume and Re-run on a card from another machine are the
exception. Their command leaves a run engine going after it returns, and
herdr-core's `exec` ends every process it started when the command returns,
so the engine would die at once and the run would use up a round. These run
instead in a short-lived herdr-core terminal on that machine (`hc run --on
<machine> --cwd <path> --outlive-parent --no-parent -- tuckerbox …`). The
board waits for the command to return, shows its output or error, and then
removes the finished terminal. The app's own `exec` on this computer leaves
the engine alone, so this computer's cards still use it.

## Forge shipping

The board document adds `ship` (`local`, `mr`, `merge`). Cards may add `mr`
(`link`, `number`, `state`), `ci` (`state`, `commit`), `ship_round` and
`max_rounds`. The JSON version stays 1. Shipping sits before Ready in either
layout; waiting and fix steps remain there, along with blocked shipping runs.
Merged runs appear in Done before the base archive is fetched. The board reads
these fields from run.json and makes no forge requests.

Open merge request uses the local browser through the GPUI click context's
`open_url`; Resume is offered on Ready with an open MR in mr mode. No merge
action is exposed. Shipping is active for edit, task tick and approval gates.
