# Tasks

## 1. Run history (run-history)

- [x] 1.1 Append events to `<git-dir>/tuckerbox/history.jsonl` from the run engine; skip a torn last line on read
- [x] 1.2 `tuckerbox history <change> [--json]`
- [x] 1.3 Tests for each run-history scenario

## 2. Dependencies (change-dependencies)

- [x] 2.1 Read `depends_on` from `openspec/changes/<id>/tuckerbox.toml`; check unknown ids and cycles in `tuckerbox check`
- [x] 2.2 Wait for dependencies before taking a slot; queued reason names unmet dependencies
- [x] 2.3 Tests for each change-dependencies scenario

## 3. Change edits (change-edits)

- [x] 3.1 Resolve where edits happen: the change's worktree on this machine, else the repository checkout; print the folder; never commit
- [x] 3.2 `tuckerbox edit <change> proposal|design|model|tasks|spec:<capability>`; editor from machine `editor`, else `$VISUAL`, else `$EDITOR`; reject `editor` in repository config
- [x] 3.3 Run the editor as a child in the same process group; validate strictly when it exits and exit with the result
- [x] 3.4 `tuckerbox validate <change> [--json]` wrapping `openspec validate <change> --strict`
- [x] 3.5 `tuckerbox tasks <change> tick|untick <n.m>`: change one checkbox byte, keep line endings, refuse unknown numbers, no-op when already in that state
- [x] 3.6 Take the change's run lock in edit (while the editor is open), tasks, approve and unapprove; refuse while a run is queued or running and name its state
- [x] 3.7 `run` and `resume` refuse with "being edited" when an edit holds the lock (adds to `change-runs`' "Change already running" refusal)
- [x] 3.8 Tests for each change-edits scenario, including a byte-for-byte tick test on a CRLF tasks.md and a lock test with a run process

## 4. Approval (change-approval)

- [x] 4.1 Approval hash: SHA-256 over proposal, design, model, tasks and `specs/**/spec.md` in path order, with path and length per file; LF line endings; task checkboxes read as unchecked
- [x] 4.2 Approval state `approved`, `stale` or `none` from the `[approval]` table and the files
- [x] 4.3 `tuckerbox approve <change>`: validate strictly, refuse invalid, write `[approval]` (hash, git `user.name`, RFC 3339 time) keeping other keys and comments; say it isn't committed
- [x] 4.4 `tuckerbox unapprove <change>`: remove `[approval]`, keep the rest, no-op when absent
- [x] 4.5 `require_approval` repository key, default `true`; `tuckerbox check` fails when machine config sets it
- [x] 4.6 Run gate in `tuckerbox run`: judge the worktree's files, else the commit a new worktree comes from (the change's branch if it exists, else the base after fetch), read from git (`git cat-file`, the committed bytes); refuse `none` or `stale` before making a worktree; make the worktree at the judged commit; judge again in the engine under the change lock; mention an uncommitted approval in the checkout; `--all` skips and lists
- [x] 4.7 Tests for each change-approval scenario, including hash fixtures shared across macOS, Linux and Windows CI

## 5. Board data (board-data)

- [ ] 5.1 Derive columns from OpenSpec, approval, run state, workflow steps and the archive; Approved only when approval is required
- [ ] 5.2 Cards: title, task progress, approval (state, by, at, committed), state, step, agent, round, branch, worktree, edit folder, machine, dependencies
- [ ] 5.3 `--change <id>` detail: proposal, spec deltas, numbered tasks with checked state, rounds and findings, history
- [ ] 5.4 Normalised remote URL as the repository key
- [ ] 5.5 Tests for each board-data scenario; JSON schema snapshot test

## 6. Herdr mod (herdr-board)

- [ ] 6.1 `herdr/mods/tuckerbox-board/`: board view with header counts, columns and cards (look based on Kelpie's board), approval badge on each card
- [ ] 6.2 Gather from every connected machine with `exec({on})`; merge by change id; show unreachable machines
- [ ] 6.3 Card detail dialog: markdown proposal, tasks with tick boxes, approval, last validation result, dependencies, rounds, history
- [ ] 6.4 Run actions: Start (Approved cards only, or Proposed when approval is off), Stop, Resume, Re-run step, Follow log (tab), Open proposal (pane), Open worktree (space); show command errors
- [ ] 6.5 Edit: pane on the card's machine running `tuckerbox edit`; `tuckerbox validate --json` when it closes, kept in memory; Validate button if pane close can't be detected; hidden while queued or running
- [ ] 6.6 Tick and untick through `tuckerbox tasks`; Approve and Unapprove through `tuckerbox approve` and `unapprove`; show the "not committed" note
- [ ] 6.7 Polling: 5 s while shown, 30 s while hidden, detail on open
- [ ] 6.8 `tuckerbox herdr install` and the `doctor` mod-version check
- [ ] 6.9 Tests for each herdr-board scenario that can run without the app (action-to-command mapping, which actions each state offers)
- [ ] 6.10 `herdr-app check` passes; try it with runs on two connected machines, including edit, tick and approve

## 7. Wrap-up

- [ ] 7.1 Screenshots in README
- [ ] 7.2 Document approval in README: approve, commit and push before `tuckerbox run`; `require_approval = false` to turn it off
- [ ] 7.3 Archive this change after the implementation is accepted (after `add-core-runner`); then fold the run gate and the new config keys into `change-runs` and `repo-config`
