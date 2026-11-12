# Tasks

## 1. Run history (run-history)

- [ ] 1.1 Append events to `<git-dir>/tuckerbox/history.jsonl` from the run engine; skip a torn last line on read
- [ ] 1.2 `tuckerbox history <change> [--json]`
- [ ] 1.3 Tests for each run-history scenario

## 2. Dependencies (change-dependencies)

- [ ] 2.1 Read `depends_on` from `openspec/changes/<id>/tuckerbox.toml`; check unknown ids and cycles in `tuckerbox check`
- [ ] 2.2 Wait for dependencies before taking a slot; queued reason names unmet dependencies
- [ ] 2.3 Tests for each change-dependencies scenario

## 3. Board data (board-data)

- [ ] 3.1 Derive columns from OpenSpec, run state, workflow steps and the archive
- [ ] 3.2 Cards: title, task progress, state, step, agent, round, branch, worktree, machine, dependencies
- [ ] 3.3 `--change <id>` detail: proposal, tasks, rounds and findings, history
- [ ] 3.4 Normalised remote URL as the repository key
- [ ] 3.5 Tests for each board-data scenario; JSON schema snapshot test

## 4. Herdr mod (herdr-board)

- [ ] 4.1 `herdr/mods/tuckerbox-board/`: board view with header counts, columns and cards (look based on Kelpie's board)
- [ ] 4.2 Gather from every connected machine with `exec({on})`; merge by change id; show unreachable machines
- [ ] 4.3 Card detail dialog: markdown proposal, tasks, dependencies, rounds, history
- [ ] 4.4 Actions: Start, Stop, Resume, Re-run step, Follow log (tab), Open proposal (pane), Open worktree (space); show command errors
- [ ] 4.5 Polling: 5 s while shown, 30 s while hidden, detail on open
- [ ] 4.6 `tuckerbox herdr install` and the `doctor` mod-version check
- [ ] 4.7 `herdr-app check` passes; try it with runs on two connected machines

## 5. Wrap-up

- [ ] 5.1 Screenshots in README
- [ ] 5.2 Archive this change after the implementation is accepted (after `add-core-runner`)
