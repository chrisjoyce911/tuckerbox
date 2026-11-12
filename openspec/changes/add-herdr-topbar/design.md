# Design

## Context

See proposal.md for why. What exists:
- The board mod (`herdr/mods/tuckerbox-board/`) has views for the board window, a docked pane and the drawer (`props.view`). `tuckerbox herdr install --drawer` adds a floating block to the shown layout, cloning a shipped one first; `fix-board-safety` adds an install lock, a write-ahead journal and recovery.
- Herdr's shipped `main` layout: a `work` column whose first child is the block `tabs` (the shipped `tabs` mod, 42 px), then `work-body` (the panes and a `panel` column holding the stack `panel-body` with `files` and `changes`), then `status`. The `tabs` mod's Files and Changes buttons run `layout.select {stack: "panel-body", child}` and `layout.show {id: "panel"}`. They are hard-coded in that mod, which is read-only.
- `herdr-t3-actions` (herdr.dev) puts its control in a top-bar row as a block with `props.block`. It measures itself with `on_size_change`, divides by `settings().zoom` (the event reports zoomed pixels, layouts take logical ones), and calls `layout.resize {id, px}` only when the width changed. Its menu is `openOverlay({kind: "popup", at, width, height})`, with `at` computed under the bar from the button's bounds and the zoom.
- `add-config-ui`: the Settings view inside the board window. `add-autopilot`: `tuckerbox autopilot`, a foreground process with a per-repository lock and a marker holding its process id, machine and start time.

## Goals / Non-Goals

**Goals:**
- Tuckerbox sits next to Herdr's own controls, and costs no space where it has nothing to say.
- One click from a fresh project to Tuckerbox set up and its board open.
- Autopilot can be started and stopped without opening a terminal.

**Non-Goals:**
- Changing Herdr's shipped `tabs` mod or its Files/Changes buttons. It is read-only, and its buttons are hard-coded.
- A Herdr-wide "panels" registry entry. `tabs` remembers the open panel per space only for the panels in `shared("workspace.panels")` (Files and Changes by default); this change doesn't write that shared key, so a space switch may close the Tuckerbox panel (Risks).
- Autopilot as a background service. It stays a foreground process in a terminal, as `add-autopilot` decided.

## Decisions

### A block beside `tabs`, not inside it

`tabs` is Herdr's mod and read-only, so the bar is a sibling block in the same row, right after `tabs`:

```json
{ "id": "tuckerbox-bar", "type": "block", "size": { "px": 0 },
  "content": { "mod": "tuckerbox-board", "props": { "view": "bar", "block": "tuckerbox-bar" } } }
```

In the shipped layout `tabs` sits in the `work` column, not a row. Install then wraps it: the `tabs` block is replaced by a row `tuckerbox-topbar` of 42 px holding `tabs` (weight 1) and `tuckerbox-bar`. It records the layout's earlier bytes in the journal and the record, like the drawer. If no `tabs` block exists, the person's layout is their own design: install prints the block JSON to place by hand and changes nothing there.

It starts at 0 px and the bar sizes itself, so a bar in a space with nothing to show takes no room, and the layout never needs a second edit.

### Sizing, as `herdr-t3-actions` does

The bar's root content is an `h_flex` with a stable id and `on_size_change`. The handler rounds `width / settings().zoom` up and, when it differs from the last value, runs `layout.resize {id: props().block, px}`. Herdr keeps that size with the layout's clicks, so it survives restarts. With nothing to draw the content is empty and the block goes to 0.

### What the bar shows

The bar shares the drawer's locating (`data.js`, with `fix-board-safety`'s main-checkout and stale-read rules):

| Shown space | Bar |
|---|---|
| no git repository | nothing, 0 px |
| git repository, no `.tuckerbox/config.toml` | `Set up Tuckerbox` |
| Tuckerbox repository | `Tuckerbox` button, running and needs-you badges, `▾` |

`herdr-board`'s rule that entry points appear only with a config stays for every other entry point. Set up is the exception, by the owner's request: it is how a project starts using Tuckerbox from Herdr. The board window itself still doesn't offer `init`. The bar is the one place, and it only shows there where `init` makes sense: a git repository with no config.

### The menu

`openOverlay({mod: "tuckerbox-board", kind: "popup", at, width: 220, height, props: {view: "bar-menu"}})`. `at` is under the ▾ button: `{x: b.x + b.width - 220 * z, y: b.y + b.height + 5 * z}` from the button's click bounds and the zoom `z`. A click on ▾ while the menu is open closes it; the mod remembers when its popup closed, as `herdr-t3-actions` does, so the closing click doesn't reopen it.

Items:
- **Board:** `mod.open tuckerbox-board`, the same toggle as the key.
- **Board in side panel:** only when `tuckerbox-panel` is in the shown layout (read from `layout()`). Select-and-show, or hide when already showing, as `tabs` does for Files and Changes.
- **Settings:** opens the board window with props `{start: "settings"}`, which `add-config-ui`'s view honours.
- **Autopilot** (only when `require_approval` is true, since autopilot refuses otherwise): a status line from `board --json`'s `autopilot`, and Start or Stop.

### The side-panel board

`view: "panel"` is the docked board's layout without Undock and Close. It follows the shown space like the board window: no `folder` prop. It lives in the `panel-body` stack, so it is the same width as Files and Changes.

### Autopilot from the bar

- **Start:** `hc run --bg --name tb-ap-<12 hex> --cwd <repository> --label herdr.space=<repo> --label herdr.tab=autopilot --no-parent -- tuckerbox autopilot`, on the space's machine, through the mod's `exec`. The name is a hash of the repository's normalised remote (or its path), as `add-live-agents` names agent terminals. herdr-core refuses a second live terminal of that name, and autopilot's own lock refuses a second autopilot, so a double click starts one.
- **Stop:** `hc keys tb-ap-<hash> ctrl+c`. Autopilot handles Ctrl-C by finishing its pass and exiting; the runs it started keep going.
- **Status:** `board --json` reads autopilot's lock and marker. Running means the lock is held now: a non-blocking try that fails, then releases at once if it succeeded. A marker without the lock is a crash leftover and counts as not running. The marker gains the terminal's name when `TUCKERBOX_TERMINAL` is set (`hc` sets the terminal's name in its environment; the start command passes it if not), so the menu can tell its own autopilot from one in a person's shell, which it shows without Stop.
- Without `hc` on the space's machine, Start explains how to run `tuckerbox autopilot` in a terminal.

### Set up Tuckerbox

`terminal.open {argv: ["tuckerbox", "init"], folder: <top level>, place: "right"}` on the space's machine. The bar polls the probe (every 2 s while that pane lives, then as usual). When `.tuckerbox/config.toml` appears, it opens the board. `init` stays the only thing that writes the config, and its pane shows its own output and errors.

### Install with the existing safety

`--topbar` is a third optional step after `--keys` and `--drawer`, inside the same install lock and journal (`fix-board-safety`). It shares the drawer's clone when both are asked for, and records its blocks in the drawer record's layout entry: `{"layout", "cloned_from", "before", "after", "blocks": ["tuckerbox-drawer", "tuckerbox-bar", "tuckerbox-panel"]}`. Uninstall removes the listed blocks from a changed layout, and restores `before` for an unchanged one, as for the drawer.

### Model

None (model.md): install runs under the existing lock and journal; two autopilots are prevented by herdr-core's unique terminal name and autopilot's own lock, both already atomic.

## Risks / Trade-offs

- **[`tabs` forgets the Tuckerbox panel across spaces]** → `tabs` restores only panels listed in `workspace.panels`. Writing that shared key would change Herdr's own setting, so this change doesn't; the panel opens again from the bar's menu.
- **[A person's own layout]** → Install edits only a row with `tabs`, or wraps `tabs` in the shipped column layout; anything else is left to the person, with the JSON to paste.
- **[Herdr changes the shipped layout's ids]** → Install looks for `tabs` and `panel-body` by id; if either is gone it says so and skips that part.
- **[Popup placement differs by platform]** → The anchor is computed from Herdr's own bounds and zoom, as `herdr-t3-actions` does; the node tests check the arithmetic, and `herdr-app check` and a manual check cover the rest.
- **[Set up in a repository of someone else's]** → `init` writes a commented starter config in the checkout and never commits, as on the CLI.

## Migration Plan

Opt-in: nothing changes until `tuckerbox herdr install --topbar`. Rollback: `tuckerbox herdr uninstall`.
