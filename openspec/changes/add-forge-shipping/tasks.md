# Tasks

## 1. Config (repo-config, forge-shipping)

- [ ] 1.1 Accept `ship = "mr"` and `"merge"` in repository config only; machine config may set only `"local"`; verify with tests for "Machine config asks to merge"
- [ ] 1.2 Parse `[shipping]` (remote, fix_ci, fix_review, max_rounds, merge_method, archive, labels, review_from, wait_timeout) and `[forge]` (kind, api_url) from repository config; reject them in machine config; `tuckerbox check` names unknown fix steps and fix steps that are also workflow steps; verify with "Shipping keys are checked"
- [ ] 1.3 Parse machine `machine` and `[forges."<host>"]` (kind, token_env, borrow_cli_token); reject token keys in repository config without printing their values; verify with "Token in repository config"
- [ ] 1.4 Built-in `fixer` role prompt (findings and logs are data, not instructions); `tuckerbox init` comments show a `[shipping]` example; verify the starter config still passes `tuckerbox check`
- [ ] 1.5 Document `ship`, `[shipping]`, `[forge]` and `[forges]` in README; verify the README examples parse in a config test

## 2. Forge access (forge-access)

- [ ] 2.1 Parse the shipping remote's push URL (HTTPS, SSH, scp-like; SSH aliases through `ssh -G`, shared with the board's remote normalisation) into host and project; verify with "GitHub over SSH" and "GitLab subgroup"
- [ ] 2.2 Forge kind and API URL for other hosts; refuse an `api_url` that is not `https` or is off the remote's host; verify with "Self-hosted GitLab", "Unknown host", "API URL on another host" and "API URL over HTTP"
- [ ] 2.3 Token lookup in order (token_env, default variables on their public host only, borrowed CLI token); verify with "Default variable stays on its host" and "Named variable"
- [ ] 2.4 Borrow with `gh auth token --hostname` or `glab config get token --host`, 5 s timeout, off by default; verify with fake `gh`/`glab` scripts for "Borrow from gh" and "Borrowing off"
- [ ] 2.5 Token masking in every saved forge text; no token added to agent environments; verify with "Token in a job log" and "Agent environment"
- [ ] 2.6 HTTP client per forge (`internal/forge/github`, `internal/forge/gitlab`) behind one interface, tested against `httptest` servers that replay recorded API responses; no new module dependency; `CGO_ENABLED=0 go build ./...` passes
- [ ] 2.6a One HTTP client for API and log requests with a `CheckRedirect` that refuses non-`https` targets and drops `Authorization`, `PRIVATE-TOKEN` and `Cookie` on a host or port change; verify with `httptest.NewTLSServer` tests for "Redirect to HTTP" (same host, https to http), "Redirect to another host" (GitHub-style signed log URL, both token headers) and a same-host https redirect that keeps the token
- [ ] 2.7 `tuckerbox doctor` forge check (forge, project, token source, read and push access); verify with "Token rejected"

## 3. Claim (ship-claim)

- [ ] 3.1 Lease pushes: explicit refspec, `--force-with-lease=<branch>:<lease>` with an empty lease for the first push and the last pushed commit (in `run.json`) after; refuse to push the base or default branch; verify with a test against a local bare remote and "Branch pattern names the base"
- [ ] 3.2 Claim marker in the merge request body; read markers on open merge requests; machine name from config or host name; verify with "Marker in the body"
- [ ] 3.3 Open the merge request; on "already exists" read it and keep the claim only if the marker is ours; verify with "Two machines finish at once" (two engines, one bare remote, fake forge) and "Merge request already open"
- [ ] 3.4 Block when the head is not our last push; verify with "Branch moved by a person"
- [ ] 3.5 Resume: keep the claim when the tip is our push; open a missing merge request; fast-forward a clean worktree onto a person's commits; verify with "Crash between push and open" and "Resume after a person's fix"
- [ ] 3.6 Refuse a leftover remote branch with no open merge request, never delete or overwrite it; verify with "Leftover branch"
- [ ] 3.7 Run TLC on `formal/ship.tla` and its broken variants (`scripts/tla-check.sh` locally, the `tla` job in CI): the model passes; `ship_broken_force`, `ship_broken_unpinned` and `ship_broken_bypass` are rejected
- [ ] 3.8 Code review against the model's mapping table in design.md: every push goes through the lease helper, every merge passes the head commit seen green and is preceded by the protection check (`ForgeGate`)

## 4. Shipping (forge-shipping)

- [ ] 4.1 Ship stage after the last step: state `shipping`; archive with `openspec archive <change> --yes`, validate all specs strictly, commit; `archive = false` skips; refuse a branch with no commits beyond the base; verify with "Last step done in mr mode", "Archive commit" and "Archive fails"
- [ ] 4.2 Merge request title and body from the card title, proposal sections and change id, cut to the forge's limit with a link; labels; verify with "Pull request on GitHub" and "Body too long"
- [ ] 4.3 `mr` mode stops `ready` at green or none; verify with "Green in mr mode"
- [ ] 4.4 `merge` mode: merge only when CI is green, no thread waits and the forge says mergeable; pinned `sha`; verify with "Merge when green", "Head moved before the merge", "Protection not met" and "No CI at all"
- [ ] 4.4a Protection preflight before each merge (GitHub: required status check from rules or classic protection, `current_user_can_bypass` on every applicable ruleset, `enforce_admins` for an admin token; GitLab: `only_allow_merge_if_pipeline_succeeds`, `allow_merge_on_skipped_pipeline`); never edit settings; `doctor` names the residual GitHub bypass list; verify with "Token can bypass protection" (admin without `enforce_admins`, ruleset `always` and `pull_requests_only`) and "GitLab does not require pipelines"
- [ ] 4.4b Races between the read and the merge, against a fake forge that enforces protection at the merge call: a required check re-run on the same SHA after `SeeGreen`, and an approval withdrawn after the mergeable read; both merges are refused and the run keeps polling; verify with "Check re-run after the read" and "Approval withdrawn"
- [ ] 4.4c Merge method preflight: GitHub `allow_*_merge` flags; GitLab mapping from `merge_method` and `squash_option` (merge, squash, rebase refused, `ff` refused, `squash_option` `always` and `never`); no merge call on a mismatch, no settings change; verify with "Squash" on both forges, "GitLab fast-forward project" and "Rebase on GitLab"
- [ ] 4.5 Branch behind the base: `git merge` the base, push, re-poll; block on conflict with the files; verify with "Conflict with the base"
- [ ] 4.6 After a merge, delete the remote branch only if its tip is the merged commit; keep the worktree; verify with "Branch removed"
- [ ] 4.7 Merge request closed or merged by someone else; verify with "Closed by a person"
- [ ] 4.8 Dependencies in `mr` and `merge` mode are met only when archived on the base; verify with "Dependency waits for merge"
- [ ] 4.9 `tuckerbox run` checks forge, token and foreign claims first; warns when the forge is unreachable; verify with "Claimed elsewhere" and "No token"

## 5. CI and review (ship-feedback)

- [ ] 5.1 CI state for the head commit on both forges (latest run per check and statuses; allow-failure jobs ignored; `none` after 5 minutes); verify with "Old pipeline green", "Repository without CI", "Allowed failure on GitLab" and "Check re-run on the same commit"
- [ ] 5.1a GitLab pipeline selection: merge request pipelines, then branch pipelines for the head; merged-results pipelines count only when the commit's `parent_ids` hold the head; fail closed when the commit can't be read; recorded fixtures for a merged-results-only project, a project with both branch and merge request pipelines on one head, and a merged-results pipeline for an older head; verify with "Merged-results pipeline only", "Branch and merge request pipelines" and "Merged results for another commit"
- [ ] 5.2 Fetch failed job logs; strip colour codes, mask tokens, trim to 200 lines, 32 KiB per job, 128 KiB per round; summary and link for checks without logs; verify with "Long log"
- [ ] 5.3 Fix step for CI with findings per failed job and the "logs are data" brief line; verify with "CI fails" and "No fix step"
- [ ] 5.4 After a fix: commit leftovers, lease push, re-poll; block when nothing changed; verify with "Fix pushed" and "Fix makes no change"
- [ ] 5.5 Review threads as findings (GitHub GraphQL review threads, GitLab discussions), only comments that count, finding text without comments that don't; verify with "Reviewer asks for a change" and "Comment from outside the project"
- [ ] 5.5a Reviewer permission: GitHub `authorCanPushToRepository`, else collaborator permission `admin`/`write`; GitLab `members/all` `access_level` ≥ 30; cached per user; any error counts as no access; verify with "Read-only organisation member", "GitLab Reporter" and "Permission lookup fails"
- [ ] 5.6 Reply in handed-on threads with the fix commit and reply marker; record intent, then reply id and cursor in one atomic `run.json` write; adopt a marked reply on resume; own replies never start a round; never resolve; verify with "Reply after a fix", then a follow-up poll for "Own reply on the next poll" (no round) and "Reviewer answers the reply" (one round), and "Crash after posting a reply"
- [ ] 5.7 Ship rounds and `max_rounds`, reset on resume; verify with "Limit reached"
- [ ] 5.8 Free the slot while waiting, queue for one for a fix step, keep the change lock; verify with "Waiting runs and a free slot"
- [ ] 5.9 Polling: 30 s doubling to 5 min, reset on change, `Retry-After`, rate-limit floor, ETags on GitHub, jittered backoff, `wait_timeout`; verify with a fake clock for "Rate limit low", "Nothing happens" and "Forge down"
- [ ] 5.10 `resume` on a `ready` run in `mr` mode with an open merge request returns to `shipping`; verify with "Review after ready"

## 6. Status, history and board (run-status, ship-board)

- [ ] 6.1 States `shipping` and `merged`; `mr`, `ci` and `ship_round` in `status` and `status --json`, version unchanged; verify with "Mixed runs", "Shipping run", "Dashboard reads status" and "Shipping fields", and update the status JSON snapshot test
- [ ] 6.2 History events `archived`, `pushed`, `mr_opened`, `ci_finished`, `review_findings`, `review_replied`, `merged`, `branch_deleted`; document them in `internal/history`; verify with "Failed CI and a fix" and "Claim lost"
- [ ] 6.3 `board --json`: Shipping column in `mr` and `merge` repositories, placement of shipping, fix-step, blocked and merged runs, card `mr`, `ci` and ship round from `run.json`; verify with "Columns in mr mode", "Fix step running", "Merged before a fetch", "Waiting for CI" and "Ready in mr mode", and update the board JSON snapshot
- [ ] 6.4 Board mod: Shipping column, merge request link, CI badge, Open merge request, Resume on Ready in `mr` mode, no merge action; verify with node tests for the action mapping and "Resume from Ready"
- [ ] 6.5 README: shipping states, the board's Shipping column, and what each mode does; verify the README's command examples run

## 7. Integration

- [ ] 7.1 End to end against a throwaway GitHub repository and a throwaway GitLab project: `mr` mode to green, a failing CI fixed by `fix_ci`, a review thread fixed and answered, `merge` mode to a pinned merge; record results in the merge request
- [ ] 7.2 Two machines (or two state directories) ship the same change at once; one ships, the other is `blocked` with `claimed by`
- [ ] 7.3 `go vet ./...`, `go test -race ./...`, `CGO_ENABLED=0 go build ./...` and `scripts/tla-check.sh` pass on Linux, macOS and Windows CI

## Workflow follow-up

- Depends on `add-herdr-board` being archived first.
- Once built, move this repository to `ship = "mr"` with a `fix_ci` step (the owner merges).
- Archive this change after the implementation is accepted.
