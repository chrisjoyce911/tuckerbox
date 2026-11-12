# Tasks

## 1. Install (herdr-topbar)

- [x] 1.1 `tuckerbox herdr install --topbar`: add `tuckerbox-bar` after `tabs` in its row, or wrap `tabs` in a 42 px row `tuckerbox-topbar` in a column layout; clone a shipped layout (sharing `--drawer`'s clone); inside the install lock and journal; record the blocks in the layout entry
- [x] 1.2 Test "Top bar on Herdr's own layout" (copy of the shipped `main` layout in a temporary Herdr home, fake `herdr-app do layout.clone`)
- [x] 1.3 Test "No tabs block" (layout unchanged, JSON printed, mod installed, non-zero exit)
- [x] 1.4 Add `tuckerbox-panel` as the last child of `panel-body` when present; skip and say so otherwise
- [x] 1.5 Test "Panel beside Files and Changes"
- [x] 1.6 Uninstall removes the recorded blocks with the drawer's rules
- [x] 1.7 Test "Install then uninstall" (`--topbar --drawer`, byte for byte)
- [x] 1.8 README: `--topbar`, what the bar shows, and uninstall

## 2. The bar (herdr-topbar, herdr-board)

- [x] 2.1 Board mod `view: "bar"`: locate as the drawer does; draw nothing, Set up, or button with badges and ▾; size the block with `layout.resize` from `on_size_change` divided by zoom, only on change
- [x] 2.2 Node test "Space without a repository" (strict API stub: empty render and `layout.resize` to 0)
- [x] 2.3 Node test "Zoomed app" (150 px at zoom 1.25 resizes to 120)
- [x] 2.4 Node test "Badges"
- [x] 2.5 Node test "Button toggles the board"
- [x] 2.6 Node test "Toggle twice" (the key still toggles; unchanged behaviour)
- [x] 2.7 Node test "Toggle in a space without a config" and "Only set up without a config" (drawer empty, toggle refused, bar shows only Set up)
- [x] 2.8 Menu popup `view: "bar-menu"`, anchored under ▾ for the zoom, closed by a second click or a choice
- [x] 2.9 Node test "Open Settings from the bar"
- [x] 2.10 `view: "panel"` and Board in side panel (`layout.select` and `layout.show`, or `layout.hide` when showing)
- [x] 2.11 Node test "Board beside the terminals"
- [x] 2.12 Set up Tuckerbox: `terminal.open` running `tuckerbox init` at the top level on the space's machine; poll for the config, then open the board
- [x] 2.13 Node test "New project"
- [x] 2.14 Node test "Init fails"
- [x] 2.15 Node test "Set up writes through init" (the bar calls no file-writing API)

## 3. Autopilot (herdr-topbar, board-data)

- [x] 3.1 `board --json` `autopilot`: running from a non-blocking lock probe, process id, machine, start time and terminal from the marker; the marker records `TUCKERBOX_TERMINAL`
- [x] 3.2 Test "Autopilot running"
- [x] 3.3 Test "Marker left by a crash"
- [ ] 3.4 Menu Autopilot: status, Start (`hc run --bg --name tb-ap-<hash> … tuckerbox autopilot`), Stop (`hc keys … ctrl+c`), no Stop for one started elsewhere, no item with approval off; a hint without `hc`
- [x] 3.5 Node test "Start and stop" (fake exec records the `hc` calls)
- [x] 3.6 Node test "Started in a shell"
- [x] 3.7 Node test "Approval off"

## 4. Integration

- [ ] 4.1 `herdr-app check herdr/mods/tuckerbox-board` passes; by hand in Herdr: the bar is invisible outside repositories, Set up works in a fresh repository, the menu sits under ▾ at zoom 1 and 1.25, and autopilot starts and stops
- [ ] 4.2 `go vet ./...`, `go test -race ./...`, `node --test herdr/mods/tuckerbox-board/test/*.test.js`, `CGO_ENABLED=0 go build ./...` and `scripts/tla-check.sh` pass on Linux, macOS and Windows CI

## Workflow follow-up

- Archive this change after the implementation is accepted.
