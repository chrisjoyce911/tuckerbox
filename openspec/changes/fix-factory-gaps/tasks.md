# Tasks

## 1. Priority publishing (priority-publish, change-priority)

- [ ] 1.1 Factor the approvals ref publish routine (fetch by name, one commit with the fetched parent, `--force-with-lease`, 5 retries with jitter, offline queue, `doctor` check) so both refs use it
- [ ] 1.2 `refs/tuckerbox/priority` with `priority.toml`; moves in solo and fleet mode plan against the fetched order, write the ref and nothing in the checkout; on a refused push re-plan the move (change, target) on the new order
- [ ] 1.3 Priority order, admission order and `board --json` read the ref (board at most once a minute, cached); ref entry wins, else the checkout sidecar
- [ ] 1.4 Offline moves: kept on the local ref, remembered in the state directory, re-planned and published on the next move or board fetch; board shows `not published`; no publishing state without a remote
- [ ] 1.5 Team mode keeps writing the sidecar; the ref doesn't count
- [ ] 1.6 `tuckerbox check` warns about uncommitted `priority` lines; the first published move includes the whole current order and says the checkout lines can be discarded
- [ ] 1.7 Tests: "Move from the board" (checkout clean), "Two moves at once" (two clones of one bare remote), "Moved on one machine, seen on another", "Old value in the sidecar", "Reorder on a train", "Ref entry in a team repository", "Leftover edits", "Reorder a queued change", "Reorder an approved change"

## 2. Closing runs (run-closing, change-runs)

- [ ] 2.1 Run states `merged` and `closed` with reasons and history events; closing takes the change's run lock and only closes a run with no agent; a queued run gives up its queue place; worktree and branch kept
- [ ] 2.2 Detect a change archived on the fetched default branch (folder gone, archive entry present) or a merged merge request; autopilot checks every pass, `status` and `board --json` at most once a minute; a missing folder without an archive is reported, not closed
- [ ] 2.3 `tuckerbox close <change> [--reason]`; refuses a running run and a change with no run; board Close action on cards with no agent
- [ ] 2.4 `tuckerbox stop` on a run with no agent marks it `stopped` and exits 0
- [ ] 2.5 Autopilot never starts, resumes or closes again a `merged` or `closed` run; `tuckerbox run` starts a new run of an active change
- [ ] 2.6 `status` and `status --json` list `merged` and `closed` runs only with `--all`; the board shows no run for them
- [ ] 2.7 Tests: "Archived on main by hand", "Status notices", "Archive entry present", "Merged by hand while blocked", "Agent still running", "Folder deleted without an archive", "Clear a blocked run", "Close while running", "Stop a blocked run", "Autopilot after a close", "Default listing"
- [ ] 2.8 Run TLC on `formal/autopilot.tla` and its broken variants with `scripts/tla-check.sh`: the model passes, `autopilot_broken_closeunseen` and the others are rejected

## 3. Conflicts while shipping (ship-conflicts)

- [ ] 3.1 `internal/forge`: merge state of a merge request on GitHub (`mergeable`, `mergeable_state`) and GitLab (`has_conflicts`, `detailed_merge_status`), with `null`/not computed polled again
- [ ] 3.2 Each poll checks the merge state before CI in `mr` and `merge` mode; a conflict starts a conflict round and doesn't count toward `wait_timeout`
- [ ] 3.3 Conflict round: take a slot, fetch the base, `git merge` it into the branch; commit a clean merge; otherwise run the fix step with the conflicting files and both sides; check no unmerged path, no conflict marker and the base as a parent; push with the lease; free the slot; wait for CI
- [ ] 3.4 Count each conflict round against `max_rounds`; block with `ship limit reached` at the limit, and with `merge conflict: <files>` (merge aborted, nothing pushed, `needs_you`) when the fixer can't resolve it
- [ ] 3.5 Tests with a fake forge: "Conflicting pull request on GitHub", "Not computed yet", "Clean merge", "Fixer resolves the conflict", "Marker left behind", "Base keeps moving", "Fixer gives up", and the GitLab merge state

## 4. CI

- [ ] 4.1 `.github/workflows/ci.yml`: concurrency group `ci-${{ github.event_name == 'pull_request' && github.ref || github.sha }}`, so pull requests cancel superseded runs and every push to `main` gets its own run that is never cancelled
- [ ] 4.2 (ci) Two quick merges to `main` both get completed CI runs

## 5. Owner tasks (owner-tasks)

- [ ] 5.1 Parse the `(owner)` marker next to `(ci)` and `(ship)`; builders' brief says not to tick owner tasks
- [ ] 5.2 Status, status --json and the card show `needs you: <numbers>` for open owner tasks
- [ ] 5.3 Test "Owner task listed"
- [ ] 5.4 Autopilot and `run --tasks next` block with `owner tasks: <numbers>` instead of starting a step
- [ ] 5.5 Test "Only hand checks left"
- [ ] 5.6 Test "Owner ticks the checks"
- [ ] 5.7 Autopilot keeps a run blocked or failed when nothing changed since it ended (branch, default branch, tasks, answers, approval)
- [ ] 5.8 Test "Same block twice"

## 6. Integration

- [ ] 6.1 README: priority publishes at once; `tuckerbox close`; merged and closed runs; conflict rounds
- [ ] 6.2 (ci) `go vet ./...`, `go test -race ./...`, `node --test herdr/mods/tuckerbox-board/test/*.test.js`, `CGO_ENABLED=0 go build ./...` and `scripts/tla-check.sh` pass on Linux, macOS and Windows CI
- [ ] 6.3 (ship) Archive the change
