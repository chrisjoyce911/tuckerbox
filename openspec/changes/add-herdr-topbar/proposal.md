# Proposal

## Why

Tuckerbox's ways into Herdr are a toggle key, a floating drawer and a docked
pane. None sits where Herdr's own controls are: the top bar, beside the
tabs, where Files and Changes open the side panel. And nothing in Herdr lets
the owner start using Tuckerbox in a project: `tuckerbox init` is CLI only,
and every entry point hides in a repository without a config.

The herdr.dev mod `herdr-t3-actions` shows how a mod can live in the top
bar: a layout block in the bar's row that sizes itself with `layout.resize`
after `on_size_change`, and opens its menu as a popup overlay under the bar.
We borrow the technique, not the code.

## What Changes

- **`tuckerbox herdr install --topbar`** adds a `tuckerbox-bar` block,
  the board mod's bar view, right after the `tabs` block in the shown
  layout. It also adds a `tuckerbox-panel` board to the side panel's
  stack. A shipped layout is cloned first, as `--drawer` does. Install and
  uninstall use the same lock, journal and record (`fix-board-safety`).
- **The bar sizes its own block:** zero pixels when the shown space has no
  git repository, so it is invisible there.
- **In a Tuckerbox repository** the bar shows a Tuckerbox button, a ▾ menu,
  and badges for running changes and changes that need you.
  - The button toggles the board window.
  - The menu has Board, Board in side panel (beside Files and Changes),
    Settings (`add-config-ui`'s view), and Autopilot: start, stop and
    whether it runs.
- **In a git repository without a config** the bar shows one item, Set up
  Tuckerbox. It runs `tuckerbox init` in a pane on the space's machine, then
  opens the board. That is the one entry point allowed where there is no
  config; the toggle, drawer and Dock still hide there.
- **`board --json` reports autopilot:** whether it runs, its process,
  machine, since when, and its terminal if the bar started it.

## Capabilities

### New Capabilities

- `herdr-topbar`: installing the top bar, what it shows in each kind of
  space, its menu, the side-panel board, autopilot from the bar, and
  setting up a repository.

### Modified Capabilities

- `herdr-board`: `Opening and hiding the board` and `Entry points need a
  Tuckerbox config` allow the top bar's Set up Tuckerbox in a git
  repository without a config.
- `board-data`: the board document says whether autopilot runs.

## Impact

- Depends on `fix-board-safety` (install lock and journal), `add-config-ui`
  (Settings view) and `add-autopilot` (the autopilot command and marker).
- `internal/herdrmod`: the `--topbar` install and uninstall steps.
- Board mod: `view: "bar"` and `view: "panel"`, the menu popup, the
  autopilot items.
- `board --json`: `autopilot`.
- Needs herdr-core (`hc`) on the space's machine to start autopilot from the
  bar; without it the item explains how to run it.
