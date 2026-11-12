# Tasks

## 1. Install safety (board-install)

- [ ] 1.1 Exclusive OS lock on `.tuckerbox-install.lock` in the Herdr home around the whole of install and uninstall (record read to record write), 30 s wait, using the change lock's helper
- [ ] 1.2 Test "Keys and drawer installed at once" (two installs started together; record has both; uninstall removes both)
- [ ] 1.3 Test "Lock held too long" (fake clock or held lock; no file changes)
- [ ] 1.4 In-memory journal of each file's earlier bytes and any clone with the layout shown before; replay it backwards on any failure after the first change; record written last
- [ ] 1.5 Test "Record write fails after the key" (forced record replacement failure)
- [ ] 1.6 Drawer step: on failure after `layout.clone`, show the earlier layout and remove the clone; if that fails, record the clone and say to run `uninstall`
- [ ] 1.7 Test "Clone unreadable" (fake clone command that writes a malformed clone)
- [ ] 1.8 Test "Clean-up fails" (clone removal forced to fail)
- [ ] 1.9 Re-install with `--keys --key <key>` different from the recorded key: clash check ignoring the board's own binding, swap the binding, record the new key, keep the first install's `keys.json` bytes
- [ ] 1.10 Test "New key"
- [ ] 1.11 Test "New key clashes"
- [ ] 1.12 Test "Uninstall after changing the key" (byte for byte)

## 2. Board view (board-view)

- [ ] 2.1 `data.js`: resolve a folder to the first `worktree` of `git worktree list --porcelain`, falling back to `--show-toplevel` when it is bare; same for a docked `folder` prop
- [ ] 2.2 Node test "Pane in a change's worktree"
- [ ] 2.3 Node test "Docked on a worktree folder"
- [ ] 2.4 Node test "Plain checkout"
- [ ] 2.5 `model.js` `repoCandidates`: a present `machine` prop wins, `null` meaning this computer; only an absent one falls back
- [ ] 2.6 Node test "Pinned to this computer in a remote space"
- [ ] 2.7 Node test "No machine prop"
- [ ] 2.8 `drawer.js` and `board.js`: tie each read to its candidate key, drop results for an old key, start a read for the current key at once
- [ ] 2.9 Node test "Drawer moves to a space without a config" (real `DrawerView`, delayed fake `exec`)
- [ ] 2.10 Node test "Board window moves to another repository" (real board view, delayed fake `exec`)
- [ ] 2.11 Board and card detail: no approval chip on Done cards
- [ ] 2.12 Node test "Archived change"
- [ ] 2.13 Node test "Active change without approval"
- [ ] 2.14 `board.js`: bound the column row to the board's width so `overflow_x_scrollbar` scrolls, columns `flex_none` at `COL_W`, header outside the row; shift with the mouse wheel scrolls sideways (handle `on_scroll_wheel` if the platform doesn't)
- [ ] 2.15 Node test "Narrow docked pane" (render test: the row is width-bound with a horizontal scrollbar, every column has fixed width 236 and none is dropped), then check by hand in a 500 px docked pane that Ready and Done are reachable
- [ ] 2.16 Node test "Shift and the mouse wheel" (the row's wheel handler moves the horizontal offset when shift is held)
- [ ] 2.17 Node test "Wide window" (no sideways offset change when the columns fit), and check by hand in the board window
- [ ] 2.18 Add `openspec/changes/add-herdr-board/tuckerbox.toml` with `title = "Herdr board"`; check every other active change has a title
- [ ] 2.19 Test "Herdr board change" (board JSON for this repository's fixture shows `Herdr board`)
- [ ] 2.20 Test "Change added without a title" (`TestRepositoryChangesHaveTitles` fails on a temporary change folder without a title)

## 3. Integration

- [ ] 3.1 `go vet ./...`, `go test -race ./...`, `node --test herdr/mods/tuckerbox-board/test/*.test.js`, `CGO_ENABLED=0 go build ./...` and `scripts/tla-check.sh` pass on Linux, macOS and Windows CI

## Workflow follow-up

- Fold `board-install` and `board-view` into `herdr-board` and `board-data` once `add-herdr-board` is archived.
- Archive this change after the implementation is accepted.
