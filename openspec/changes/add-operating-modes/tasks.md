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
- [ ] 2.11 `tuckerbox status --all-machines [--json]` with a `machine` field per run, version unchanged; update the status JSON snapshot
- [ ] 2.12 Test "Runs on two machines"

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
- [ ] 3.14 `tuckerbox run --take-over [--force]`: next epoch, start from the remote branch else the base, leave the old worktree alone
- [ ] 3.15 Test "Take over after a crash"
- [ ] 3.16 Test "Parked without force"
- [ ] 3.17 Fenced first push in the ship stage: `git push --atomic` with leases on the branch and the fleet ref (a renewal commit); block on refusal
- [ ] 3.18 Test "Old holder wakes up"
- [ ] 3.19 `ready` claims on ready runs; remove on merged; drop claims of archived changes on any write
- [ ] 3.20 Test "Ready on one machine"
- [ ] 3.21 `tuckerbox claim show [--json]` and `claim release [--force]`; `doctor` in a fleet repository reports whether the fleet ref can be fetched
- [ ] 3.22 Test "Release another machine's claim"
- [ ] 3.23 Run TLC on `formal/fleet.tla` and its broken variants (`scripts/tla-check.sh` locally, the `tla` job in CI): the model passes, both variants are rejected
- [ ] 3.24 Code review against model.md's mapping table: every fleet ref write is a lease push; the ship stage's first push in fleet mode is atomic with the fleet ref

## 4. Dispatch (fleet-placement)

- [ ] 4.1 `tuckerbox dispatch [change...] [--dry-run] [--json]`: one pass, changes in Approved order that pass the run gate and dependencies and have no claim, up to the fleet limit; document it in README with a scheduling example that uses the person's own scheduler
- [ ] 4.2 Test "Nothing to do"
- [ ] 4.3 Test "Unapproved change skipped"
- [ ] 4.4 Candidates from parallel `machine --json` reports: answered, free capacity, checkout, each step's effective agent installed and not signed out
- [ ] 4.5 Test "Agent signed out"
- [ ] 4.6 Test "Sign-in unknown"
- [ ] 4.7 Choice: preferred `machines`, then most free capacity, then name; capacity taken within the pass
- [ ] 4.8 Test "Preferred machine full"
- [ ] 4.9 Start with `tuckerbox run --for <remote> <change>` locally, over SSH or herdr-core; `run --for` finds the checkout through the machine index
- [ ] 4.10 Test "Start over SSH"
- [ ] 4.11 Report refused claims and failed starts without retrying in the pass
- [ ] 4.12 Test "Two dispatchers"
- [ ] 4.13 Test "Dry run" (fleet ref unchanged)

## 5. Team mode (team-claims)

- [ ] 5.1 Claim at start: empty `Start <change>` commit with `[skip ci]`, lease push expecting no branch, draft merge request with the marker and `by=<login>` from the forge; ship-claim's refusals
- [ ] 5.2 Test "Two people start one change" (two state directories, one bare remote, fake forge)
- [ ] 5.3 Test "Marker with the owner"
- [ ] 5.4 Treat a marker whose `by` differs from the merge request's author as someone else's claim
- [ ] 5.5 Test "Marker copied by someone else"
- [ ] 5.6 Ship stage in team mode: update the run's draft and mark it ready on GitHub and GitLab, open none
- [ ] 5.7 Test "Last step done in team mode"
- [ ] 5.8 Refuse `--take-over` and `dispatch` in team mode
- [ ] 5.9 Test "Take over refused"
- [ ] 5.10 Test "Dispatch in a team repository"
- [ ] 5.11 `approve` records the forge login; the run gate counts only `[team] approvers`
- [ ] 5.12 Test "Approval by someone not listed"
- [ ] 5.13 Test "No approvers"
- [ ] 5.14 Open claimed merge requests for the board, at most once a minute, kept in the state directory, with their age when the forge is down
- [ ] 5.15 Test "Board read twice"

## 6. Board (modes-board)

- [ ] 6.1 `board --json`: `mode`, fleet `max_runs` and claim count, `claim` on cards (fleet ref or forge), claims shown for machines not connected; update the board JSON snapshot
- [ ] 6.2 Test "Fleet board"
- [ ] 6.3 Test "Expired claim on a card"
- [ ] 6.4 Test "Owner in a team"
- [ ] 6.5 Board mod: claim badge, owner, Dispatch in the header (only with unclaimed Approved cards), Take over on expired cards with a confirmation
- [ ] 6.6 Node test "Dispatch from the board"
- [ ] 6.7 Node test "Take over an expired card"

## 7. Integration

- [ ] 7.1 Fleet end to end with two machines (or two state directories and a fake `ssh`) and one bare remote: dispatch places two changes, one machine is killed, its claim expires, the other takes over, the killed one wakes and blocks at its renewal
- [ ] 7.2 Team end to end against a throwaway GitHub repository: two people start the same change, one wins, the draft becomes ready at the ship stage
- [ ] 7.3 `go vet ./...`, `go test -race ./...`, `CGO_ENABLED=0 go build ./...` and `scripts/tla-check.sh` pass on Linux, macOS and Windows CI

## Workflow follow-up

- Depends on `add-forge-shipping` and `add-change-settings` being archived first.
- Replaces the planned `add-remote-machines`.
- Archive this change after the implementation is accepted.
