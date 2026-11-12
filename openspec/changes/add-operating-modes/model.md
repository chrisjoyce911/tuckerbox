# Model

## Model

Fleet mode adds state several machines write with no lock they share: the claims in `refs/tuckerbox/fleet` on the git remote. Machines crash, sleep and wake up unaware, and claims expire by time. The only atomic primitive is a git push with `--force-with-lease` (with `--atomic` across several refs), so the claim protocol is modelled.

Machine-wide fleet slots need no new model: they are the slot locks `formal/runs.tla` checks (`WithinLimit`), in a machine-wide folder. Team mode needs no new model either. Its claim is `add-forge-shipping`'s, taken earlier: the empty start commit's push is `formal/ship.tla`'s first `Push`, and opening the draft is its `Open`. `OneClaim` and `NoLostPush` cover it (see design.md, "Team mode").

### Files

| File | Role |
|---|---|
| `formal/fleet.tla`, `formal/fleet.cfg` | Model; TLC must pass |
| `formal/fleet_broken_plainclaim.tla`, `.cfg` | Broken variant: a claim pushes without `--force-with-lease`, deciding from a stale fetch; TLC must reject (`OneHolderPerEpoch`) |
| `formal/fleet_broken_unfenced.tla`, `.cfg` | Broken variant: branch pushes lease only the branch, not the fleet ref; TLC must reject (`FencedPush`) |
| `formal/fleet_broken_nomove.tla`, `.cfg` | Broken variant: a take-over of a pushed branch adopts its tip instead of moving it; TLC must reject (`FencedMerge`) |
| `formal/fleet_broken_releasekeep.tla`, `.cfg` | Broken variant: `claim release --force` removes the claim but leaves the branch tip; TLC must reject (`FencedMerge`) |

### Actors and shared state

- **Machines (`Machines`):** each fetches, claims or takes over a change, renews, pushes the branch any number of times (the first push and fix rounds), sees its head green, merges pinned to that commit, and releases.
- **A person (`ForceRelease`):** from any machine, at any moment (also while the holder is shipping or about to merge), releases a change's claim with `claim release --force`.
- **Time (`Expire`):** marks a claim expired at any moment, whatever its holder is doing. No clock is modelled, so safety can't depend on one.
- **Pauses:** a machine that is slow, asleep or cut off is one whose actions aren't scheduled for a while. Interleaving already lets any machine stop between any two of its actions, before or after shipping starts, and resume later unaware; a machine that never comes back never acts again. So there is no separate crash action.
- **Shared state on the remote and forge:**
  - `version`: the fleet ref's commit, as a counter
  - `claim[c]`: holder and epoch per change
  - `tip[c]`: the change branch's tip (0: no branch)
  - `merged[c]`: the forge merged the change
- **Per machine:** `seen`, `snap`, `snapTip` (what it last fetched), `job` and `epoch` (the claim it believes it holds), `lease` (the tip it last pushed), `checked` (the commit it saw green), `pc`.
- **Atomic:**
  - `Claim`: fleet ref lease and branch lease in one push; with a branch, also the take-over commit
  - `Push`: branch lease and fleet ref lease in one push
  - `ForceRelease`: fleet ref lease and branch lease in one push; with a branch, also the release commit
  - `Merge`: pinned to `checked`; the forge refuses it if `tip` moved
- **Not atomic:** fetching and then deciding; seeing CI green and then merging; everything a machine does between renewals.
- **Abstractions:**
  - `Renew` and `Release` act on the current ref in one step. In the code they fetch, check and lease-push, and retry on refusal, which comes to the same.
  - CI is not modelled: `SeeGreen` may pick any commit the machine pushed. `formal/ship.tla` checks that only green commits merge.
  - A parked claim is a claim whose `Expire` never fires, a subset of the modelled behaviours.
  - `Release` frees a merged change; `Claim` refuses merged changes.

### Invariants

| Invariant | Meaning | Named by requirement |
|---|---|---|
| `OneHolderPerEpoch` | Each claim on a change, first or take-over, is handed to exactly one machine | fleet-claims: Claiming at run start |
| `FencedPush` | Only the machine holding the claim ever moves the change's branch | fleet-claims: Fenced branch pushes |
| `FencedMerge` | Only the machine holding the claim ever has a merge accepted | fleet-claims: Fenced merges, Take-over moves the branch, Releasing moves the branch |
| `WithinFleetLimit` | The fleet ref never holds more claims than `max_runs` | fleet-claims: Fleet limit |
| `TypeOK` | Variables keep their types | (type check) |

### Bounds

`Machines = {m1, m2}`, `Changes = {c1, c2}`, `MaxRuns = 1`, `MaxEpoch = 2`, `MaxVersion = 6`, `MaxCommits = 3`.
- Two machines cover every race here: two claims from one fetch, and an old holder against the machine that took over.
- Two changes with `MaxRuns = 1` exercise the limit.
- `MaxEpoch = 2` allows a first claim and one take-over per change.
- `MaxCommits = 3` allows the old holder's push, the take-over commit, and one more push by either machine.
- `MaxVersion = 6` allows a claim, a push, a take-over, a renewal, a push and a release in one behaviour.
- `Finished` lets TLC stop once every machine is done or the version bound is used up, so the bound is not reported as a deadlock.

TLC (tla2tools 1.7.4, `scripts/tla-check.sh`) explores 15 140 distinct states in a few seconds. The passing model is not vacuous: with a temporary invariant "no change merges after a take-over of its pushed branch", TLC finds the 9-state behaviour where `m1` pushes, its claim expires, `m2` takes over with a take-over commit, sees it green and merges.
- `fleet_broken_plainclaim` is rejected after 5 states: `m1` and `m2` fetch, then both claim `c1` with epoch 1.
- `fleet_broken_unfenced` is rejected after 6 states: `m1` claims `c1`, a person force-releases it before any push, and `m1`, unaware, creates the branch. (A take-over before the first push gives the same violation a step later.)
- `fleet_broken_nomove` is rejected after 9 states: `m1` pushes and sees its head green, the claim expires, `m2` takes over without moving the tip, and `m1` merges.
- `fleet_broken_releasekeep` is rejected after 8 states: `m1` pushes and sees its head green, `m2` fetches and force-releases the claim without moving the tip, and `m1` merges.
