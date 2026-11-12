# Tasks

## 1. Config and modes (repo-config, operating-modes)

- [ ] 1.1 Parse `mode`, `[fleet]` (`max_runs`, `lease`) and `[team]` (`approvers`) from repository config only; parse `fleet_capacity` and `[machines.<name>]` (`ssh` or `hc`) from machine config only; `tuckerbox init` comments show examples; document them in README
- [ ] 1.2 Test "Machine config sets a fleet limit"
- [ ] 1.3 Test "Repository lists machines"
- [ ] 1.4 Test "Default mode"
- [ ] 1.5 Test "Machine config sets the mode"
- [ ] 1.6 `tuckerbox check` per mode: fleet needs a remote; team needs `ship = "mr"` or `"merge"` and approvers when approval is required; list leftover fleet claims
- [ ] 1.7 Test "Fleet without a remote"
- [ ] 1.8 Test "Team with local shipping"
- [ ] 1.9 Test "Fleet to solo with a claim left"
- [ ] 1.10 Test "Dispatch in a solo repository"

## 2. Machines (machine-inventory)

- [ ] 2.1 Remote command runner: `ssh -T -o BatchMode=yes <dest> -- tuckerbox ...` or `hc run --on <name> -- tuckerbox ...`, timeout, no credentials sent; tested with fake `ssh` and `hc` scripts
- [ ] 2.2 Test "Machine reached by herdr-core"
- [ ] 2.3 Test "Both ways given"
- [ ] 2.4 Test "Machine asleep"
- [ ] 2.5 Sign-in check for built-in agents in `doctor` (signed in, signed out, unknown; 5 s timeout, no session); README drops "sign-in not checked"
- [ ] 2.6 Test "Unknown sign-in"
- [ ] 2.7 `tuckerbox machine --json [--for <remote>]`: name, version, OS, `fleet_capacity` and fleet runs, agents installed and signed in, checkout via the machine index
- [ ] 2.8 Test "Machine not in the fleet"
- [ ] 2.9 Test "Codex signed out"
- [ ] 2.10 Test "No checkout"
- [ ] 2.11 Machine-wide fleet slots: `run --dispatched` takes one of `fleet_capacity` OS file locks under the machine state directory before claiming, holds it while queued, running or shipping, frees it on end, stop or block; `resume` of a dispatched run takes one again; refuse with `<machine> is full`
- [ ] 2.12 Test "Two repositories, one place" (two processes from two repositories, `fleet_capacity = 1`; one claims, the other refuses and claims nothing)
- [ ] 2.13 Test "Person's own run" (no fleet slot taken)
- [ ] 2.14 `tuckerbox status --all-machines [--json]` with a `machine` field per run, version unchanged; update the status JSON snapshot
- [ ] 2.15 Test "Runs on two machines"

## 3. Fleet claims (fleet-claims)

- [ ] 3.1 Fleet ref read and write: fetch `refs/tuckerbox/fleet` by name, one TOML file per claim, every write a `--force-with-lease` push on the fetched version, fetch and re-decide on refusal; tested against a local bare remote
- [ ] 3.2 Test "Two machines write at once" (two state directories, one bare remote)
- [ ] 3.3 Claim in `tuckerbox run` after the run gate and before the change lock and worktree, with the next epoch; refuse live claims held elsewhere
- [ ] 3.4 Test "Two machines pick the same change"
- [ ] 3.5 Test "Claimed elsewhere"
- [ ] 3.6 Fleet limit in the same compare-and-swap; count active and parked claims
- [ ] 3.7 Test "Limit reached"
- [ ] 3.8 Renewal from the run's process every `lease`/5 while queued, running or shipping; on a lost claim stop the agent and block with `claim taken by <machine>`; tested with a fake clock
- [ ] 3.9 Test "Claim taken while running"
- [ ] 3.10 Park on stop, block and fail; re-activate on `resume` and `rerun`, refusing a claim no longer ours
- [ ] 3.11 Test "Blocked overnight"
- [ ] 3.12 Expiry from `renewed_at`, `lease` and a 1-minute skew allowance; never removed automatically
- [ ] 3.13 Test "Machine crashed"
- [ ] 3.14 `tuckerbox run --take-over [--force]` without a remote branch: next epoch, start at the first step from the base, leave the old worktree alone
- [ ] 3.15 Test "Take over before shipping"
- [ ] 3.16 Test "Parked without force"
- [ ] 3.17 Take-over with a remote branch: one atomic push of the fleet claim and an empty `Take over <change> from <machine>` commit, leasing both refs at what was fetched; refetch on refusal
- [ ] 3.18 Test "Branch moved during take-over"
- [ ] 3.19 Every branch push in fleet mode (first and fix rounds) is `git push --atomic` with leases on the branch and the fleet ref (a renewal commit); block on refusal
- [ ] 3.20 Test "Old holder wakes before its first push"
- [ ] 3.21 Test "Old holder pushes a CI fix after take-over"
- [ ] 3.22 Renew just before a merge and don't merge without the claim; the merge stays pinned to the commit seen green
- [ ] 3.23 Test "Old holder merges after take-over" (fake forge that refuses a pinned merge when the tip moved)
- [ ] 3.24 Marker `epoch=<n>` in fleet mode; a run whose fleet epoch is higher than the marker's, and whose push last moved the branch, rewrites the marker; `run`'s early claim check and ship-claim's refusals defer to the fleet claim in fleet mode
- [ ] 3.25 Test "Marker of the old holder"
- [ ] 3.26 Test "Crash before the marker is rewritten"
- [ ] 3.27 After a take-over of a pushed branch, make the worktree from the take-over commit and continue in the ship stage: no step re-runs, no second archive, open a merge request if none is open
- [ ] 3.28 Test "Take-over while waiting for CI" (end to end: two state directories, one bare remote, fake forge; the old holder's fix push and pinned merge are both refused)
- [ ] 3.29 Test "Branch without a merge request"
- [ ] 3.30 `ready` claims on ready runs; remove on merged; drop claims of archived changes on any write
- [ ] 3.31 Test "Ready on one machine"
- [ ] 3.32 `tuckerbox claim show [--json]` and `claim release [--force]`; `doctor` in a fleet repository reports whether the fleet ref can be fetched
- [ ] 3.33 Test "Release another machine's claim"
- [ ] 3.34 Release with a remote branch (with or without `--force`): one atomic push removing the claim and moving the branch to an empty `Release <change> from <machine>` commit, leasing both; without a branch, the fleet ref only; the old holder blocks with `claim released`
- [ ] 3.35 Test "Forced release while the holder is about to merge" (two state directories, bare remote, fake forge refusing a pinned merge when the tip moved)
- [ ] 3.36 Test "Release before any push"
- [ ] 3.37 Any restart of a ready run with another scope or none, on the holder's machine: one lease push turns the claim active (same epoch) with the fleet limit in the same compare-and-swap; `fleet limit reached` keeps run and claim ready; refuse from another machine
- [ ] 3.38 Test "Next group on the same machine"
- [ ] 3.39 Test "Rest of the change after a scope" (`run` with no `--tasks` reuses and reactivates the claim)
- [ ] 3.40 Test "Room taken meanwhile" (two state directories, bare remote, `max_runs = 1`)
- [ ] 3.41 Test "Next scope from another machine"
- [ ] 3.42 Next scope of a stopped, blocked or failed run reactivates its parked claim as `resume` does; record `dispatched` in `run.json`; free the fleet slot on ready; a dispatched run's next scope takes a fleet slot before the claim
- [ ] 3.43 Test "Next scope after a block"
- [ ] 3.44 Test "Dispatched run on a full machine"
- [ ] 3.45 Run TLC on `formal/fleet.tla` and its broken variants (`scripts/tla-check.sh` locally, the `tla` job in CI): the model passes, all five variants are rejected
- [ ] 3.46 Code review against model.md's mapping table: every fleet ref write is a lease push; every branch push, take-over and release in fleet mode is atomic with the fleet ref; every merge is pinned; dispatched runs take a fleet slot before claiming; a next scope's reactivation checks the limit

## 4. Dispatch (fleet-placement)

- [ ] 4.1 `tuckerbox dispatch [change...] [--dry-run] [--json]`: one pass, changes in Approved order that pass the run gate and dependencies and have no claim, up to the fleet limit; document it in README with a scheduling example that uses the person's own scheduler
- [ ] 4.2 Test "Nothing to do"
- [ ] 4.3 Test "Unapproved change skipped"
- [ ] 4.4 Candidates from parallel `machine --json` reports: answered, free capacity, checkout, each step's effective agent installed and not signed out
- [ ] 4.5 Test "Agent signed out"
- [ ] 4.6 Test "Sign-in unknown"
- [ ] 4.7 Choice: preferred `machines`, then most free capacity, then name; capacity taken within the pass
- [ ] 4.8 Test "Preferred machine full"
- [ ] 4.9 Start with `tuckerbox run --for <remote> --dispatched <change>` locally, over SSH or herdr-core; `run --for` finds the checkout through the machine index
- [ ] 4.10 Test "Start over SSH"
- [ ] 4.11 Report refused claims and failed starts without retrying in the pass
- [ ] 4.12 Test "Two dispatchers"
- [ ] 4.13 Test "Dry run" (fleet ref unchanged)
- [ ] 4.14 Continuations in dispatch: approved changes with a `ready` claim and an incomplete run, continued on the holder only with `--tasks next` when scoped or `[autopilot] scope = "group"`, else reported; never complete ready runs or parked claims; continuations first, then new changes; read run state, `since`, scope, completeness and `dispatched` from the holder's machine report (extend `machine --json --for`)
- [ ] 4.15 Test "Next group across passes" (two state directories, bare remote, `max_runs = 1`: first pass reports and starts nothing, second pass after the other change is ready continues on the holder)
- [ ] 4.16 Test "Complete ready run"
- [ ] 4.17 Test "Stopped by a person"
- [ ] 4.18 Test "Continuations first"
- [ ] 4.19 Test "Holder unreachable"
- [ ] 4.20 `tuckerbox run --expect-state <s> --expect-since <t>`: after the fleet slot and under the change lock, check state, `since` and `approved` before any run or claim change; change nothing on a mismatch; dispatch passes what the holder's report said and reports mismatches (shares the engine check of `add-autopilot` task 3.1)
- [ ] 4.21 Test "A person reran and stopped it meanwhile" (two state directories, bare remote; the person's rerun and stop land between the report and the command)
- [ ] 4.22 Test "Unapproved before the command arrives"

## 5. Team mode (team-claims)

- [ ] 5.1 Claim at start: empty `Start <change>` commit with `[skip ci]`, lease push expecting no branch, draft merge request with the marker and `by=<login>` from the forge; ship-claim's refusals
- [ ] 5.2 Test "Two people start one change" (two state directories, one bare remote, fake forge)
- [ ] 5.3 Test "Marker with the owner"
- [ ] 5.4 Treat a marker whose `by` differs from the merge request's author as someone else's claim
- [ ] 5.5 Test "Marker copied by someone else"
- [ ] 5.6 Team restart with another scope or none: check the open draft's marker and login and that the remote tip is the run's last push; start with no new commit and no push; refuse with ship-claim's reasons; run gate from the protected base
- [ ] 5.7 Test "Next group in a team" (fake forge: no push, same draft open)
- [ ] 5.8 Test "Rest of the change in a team" (no push, same draft open)
- [ ] 5.9 Test "Draft closed between scopes"
- [ ] 5.10 Test "Someone else's next scope"
- [ ] 5.11 Ship stage in team mode: update the run's draft and mark it ready on GitHub and GitLab, open none
- [ ] 5.12 Test "Last step done in team mode"
- [ ] 5.13 Refuse `--take-over` and `dispatch` in team mode
- [ ] 5.14 Test "Take over refused"
- [ ] 5.15 Test "Dispatch in a team repository"
- [ ] 5.16 `approve` records the forge login; in team mode the run gate reads the approval only from the fetched base commit, requires a listed login, and refuses worktrees or retained branches whose files don't match it
- [ ] 5.17 Test "Approval by someone not listed"
- [ ] 5.18 Test "No approvers"
- [ ] 5.19 Test "Forged approval on a retained branch" (local branch with recomputed hashes and a listed login; nothing created locally or on the fake forge)
- [ ] 5.20 Test "Worktree edited after approval"
- [ ] 5.21 Team mode `resume` and `rerun`: compare the `agent` and `permissions` overrides with `settings_hash` from the approval on the fetched base (listed login), never the worktree's; refuse on mismatch; a stale text hash alone still passes
- [ ] 5.22 Test "Forged settings approval on resume"
- [ ] 5.23 Test "Forged settings approval on rerun"
- [ ] 5.24 Test "Design rewritten by a plan step"
- [ ] 5.25 Ask the forge whether the base branch refuses direct pushes and needs an approving review (GitHub branch rules, GitLab protected branches and approval rules); refuse in `run` and fail `check` when not, or when unreadable
- [ ] 5.26 Test "Unprotected base"
- [ ] 5.27 Test "Protection can't be read"
- [ ] 5.28 Open claimed merge requests for the board, at most once a minute, kept in the state directory, with their age when the forge is down
- [ ] 5.29 Test "Board read twice"

## 6. Board (modes-board)

- [ ] 6.1 `board --json`: `mode`, fleet `max_runs` and claim count, `claim` on cards (fleet ref or forge), claims shown for machines not connected; update the board JSON snapshot
- [ ] 6.2 Test "Fleet board"
- [ ] 6.3 Test "Expired claim on a card"
- [ ] 6.4 Test "Owner in a team"
- [ ] 6.5 Board mod: claim badge, owner, Dispatch in the header (only with unclaimed Approved cards), Take over on expired cards with a confirmation
- [ ] 6.6 Node test "Dispatch from the board"
- [ ] 6.7 Node test "Take over an expired card"

## 7. Integration

- [ ] 7.1 Fleet end to end with two machines (or two state directories and a fake `ssh`) and one bare remote: dispatch places two changes, one machine is killed during its steps and another while waiting for CI, their claims expire, the other machine takes both over, and the killed ones wake and are refused at their next push, merge or renewal
- [ ] 7.2 Team end to end against a throwaway GitHub repository: two people start the same change, one wins, the draft becomes ready at the ship stage
- [ ] 7.3 `go vet ./...`, `go test -race ./...`, `CGO_ENABLED=0 go build ./...` and `scripts/tla-check.sh` pass on Linux, macOS and Windows CI

## Workflow follow-up

- Depends on `add-forge-shipping` and `add-change-settings` being archived first.
- Replaces the planned `add-remote-machines`.
- Archive this change after the implementation is accepted.
