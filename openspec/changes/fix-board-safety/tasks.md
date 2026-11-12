# Tasks

## 1. Install safety (board-install)

- [ ] 1.1 Exclusive OS lock on `.tuckerbox-install.lock` in the Herdr home around the whole of install and uninstall (record read to record write), 30 s wait, using the change lock's helper
- [ ] 1.2 Test "Keys and drawer installed at once" (two installs started together; record has both; uninstall removes both)
- [ ] 1.3 Test "Lock held too long" (fake clock or held lock; no file changes)
- [ ] 1.4 Journal file `.tuckerbox-install.journal.json`: written durably (temp file, sync, rename) before the first change with each entry's before bytes and the hash to be written; clone entries written before `layout.clone` and updated with the clone's hash after; removed after the record is written or everything is undone
- [ ] 1.5 Test "Journal can't be written" (read-only Herdr home; nothing changes)
- [ ] 1.6 On failure, undo the journal's entries backwards; record written last
- [ ] 1.7 Test "Record write fails after the key" (forced record replacement failure; journal gone afterwards)
- [ ] 1.8 Recovery at the start of every install and uninstall, under the lock: undo entries still as written (idempotent), report and drop entries the person changed since, then remove the journal; on any undo I/O failure keep the journal with the entries left, exit non-zero and refuse to start any new change
- [ ] 1.9 Fault-injection test "Record write and rollback both fail" (record replacement always fails and restoring `keys.json` fails; the journal remains and names `keys.json`)
- [ ] 1.10 Test "Uninstall finishes the undo" (faults cleared after 1.9; `keys.json` byte for byte as before; journal gone)
- [ ] 1.11 Test "File edited since" (entry dropped and reported, file untouched, journal removed)
- [ ] 1.12 Fault-injection test "Fault still there on the next try" (journal from 1.9 with the fault kept; uninstall then `install --drawer` both exit non-zero naming `keys.json`, journal byte for byte unchanged, no layout, key or mod file changes)
- [ ] 1.13 Test "Fault cleared after a failed recovery" (after 1.12, clear the fault; uninstall restores `keys.json` byte for byte, removes the journal and finishes)
- [ ] 1.14 Drawer step: on failure after `layout.clone`, show the earlier layout and remove the clone; if that fails, record the clone (`cloned_from`, empty `before`, `after` = its hash as found)
- [ ] 1.15 Test "Clone unreadable" (fake clone command that writes a malformed clone)
- [ ] 1.16 Test "Clean-up fails" (clone removal forced to fail; record entry as specified)
- [ ] 1.17 `uninstallDrawer`: compare the hash before parsing; remove an unchanged clone without parsing, switching back to `cloned_from` if shown; leave and report a changed layout that doesn't parse, and go on with the key and mods
- [ ] 1.18 End-to-end test "Malformed clone removed" (failed install with clean-up failure, then uninstall with Herdr running: `main` shown, clone gone, exit 0)
- [ ] 1.19 Test "Clone edited since" (clone kept, key undone, mod removed)
- [ ] 1.20 Re-install with `--keys --key <key>` different from the recorded key: clash check ignoring the board's own binding, swap the binding, record the new key, keep the first install's `keys.json` bytes
- [ ] 1.21 Test "New key"
- [ ] 1.22 Test "New key clashes"
- [ ] 1.23 Test "Uninstall after changing the key" (byte for byte)

## 2. Board view (board-view)

- [ ] 2.1 `data.js`: resolve a folder to the first `worktree` of `git worktree list --porcelain`, falling back to `--show-toplevel` when it is bare; same for a docked `folder` prop
- [ ] 2.2 Node test "Pane in a change's worktree"
- [ ] 2.3 Node test "Docked on a worktree folder"
- [ ] 2.4 Node test "Plain checkout"
- [ ] 2.5 `model.js` `repoCandidates`: a present `machine` prop wins, `null` meaning this computer; only an absent one falls back
- [ ] 2.6 Node test "Pinned to this computer in a remote space"
- [ ] 2.7 Node test "No machine prop"
- [ ] 2.8 `drawer.js` and `board.js`: `want` key; on a change drop what is shown and start a read for the new key at once; `loading` as a set of keys; apply a finished read only if its key is still `want`
- [ ] 2.9 Node test "Drawer moves to a space without a config" (real `DrawerView`, delayed fake `exec` released in a chosen order)
- [ ] 2.10 Node test "Back to the first space" (A, then B, then A; B's read finishes after A's)
- [ ] 2.11 Node test "Board window moves to another repository" (real board view, delayed fake `exec`)
- [ ] 2.12 Run TLC on `formal/boardview.tla` and its broken variant (`scripts/tla-check.sh` locally, the `tla` job in CI): the model passes, the variant is rejected
- [ ] 2.13 Board and card detail: no approval chip on Done cards
- [ ] 2.14 Node test "Archived change"
- [ ] 2.15 Node test "Active change without approval"
- [ ] 2.16 Reproduce in a 500 px docked pane why Ready and Done can't be reached, and record the cause in the implementing pull request before changing layout
- [ ] 2.17 `board.js`: fix the cause found; columns `flex_none` at 236 px in a row with `overflow_x_scrollbar()`; header outside the row; no wheel handler of its own
- [ ] 2.18 Strict Herdr API stub for tests: a committed list of the `gpui-kit.d.ts` members the board uses (with the Herdr version), a stub that throws on anything else, and a check script that compares the list with an installed `gpui-kit.d.ts` when present
- [ ] 2.19 Node test "Narrow docked pane" (render test with the strict stub: the row is `overflow_x_scrollbar`, every column is 236 px and none is dropped), then check by hand in a 500 px docked pane that dragging the scrollbar and swiping reach Ready and Done
- [ ] 2.20 Node test "Wide window" (same render test at a width where all columns fit), and check by hand in the board window that no scrollbar shows
- [ ] 2.21 Check by hand "Shift and the wheel turned sideways" on macOS (shift with the wheel scrolls the row), and record what Linux and Windows do in the implementing pull request
- [ ] 2.22 Node test "Plain wheel over a column" (render test: each column list is `overflow_y_scrollbar` and the row has no `on_scroll_wheel`), and check by hand
- [ ] 2.23 Add `openspec/changes/add-herdr-board/tuckerbox.toml` with `title = "Herdr board"`; check every other active change has a title
- [ ] 2.24 Test "Herdr board change" (board JSON for this repository's fixture shows `Herdr board`)
- [ ] 2.25 Test "Change added without a title" (`TestRepositoryChangesHaveTitles` fails on a temporary change folder without a title)

## 3. Integration

- [ ] 3.1 `go vet ./...`, `go test -race ./...`, `node --test herdr/mods/tuckerbox-board/test/*.test.js`, `CGO_ENABLED=0 go build ./...` and `scripts/tla-check.sh` pass on Linux, macOS and Windows CI

## Workflow follow-up

- Fold `board-install` and `board-view` into `herdr-board` and `board-data` once `add-herdr-board` is archived.
- Archive this change after the implementation is accepted.
