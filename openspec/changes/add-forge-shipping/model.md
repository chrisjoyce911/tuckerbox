# Model

## Model

Shipping adds state shared between machines: the change's branch and merge request on the forge. Several run processes on different machines, and people, write it, with no lock they share. The only atomic primitives are two forge operations, so the design's claim is modelled.

### Files

| File | Role |
|---|---|
| `formal/ship.tla`, `formal/ship.cfg` | Model; TLC must pass |
| `formal/ship_broken_force.tla`, `.cfg` | Broken variant: a run pushes without `--force-with-lease` (a plain or forced push after checking nobody claimed the change); TLC must reject (`NoLostPush`) |
| `formal/ship_broken_unpinned.tla`, `.cfg` | Broken variant: the merge is not pinned to the commit seen green; TLC must reject (`MergedGreen`) |
| `formal/ship_broken_bypass.tla`, `.cfg` | Broken variant: the forge does not enforce protection for the token (a bypassing token), so only Tuckerbox's earlier read counts; TLC must reject (`MergedGated`) |

### Actors and shared state

- **Machines (`Machines`):** one shipping run each, for the same change. A run pushes, opens the merge request, waits for green, merges, and pushes again after each fix round.
- **A person (`PersonPush`):** may push to the branch at any time, with no lease.
- **CI (`ReqDone`, `OptDone`, `ReqRerun`, `OptRerun`):** reports `green` or `red` on each pushed commit, for required checks (`req`) and other checks (`opt`) separately. Anyone may re-run a check on the same commit at any time, so a finished result becomes `pending` again and can finish differently.
- **Reviewers (`Approval`):** grant or withdraw the approval protection requires (`approved`), at any time.
- **Shared state on the forge:**
  - `tip`: the branch's tip commit (0: no branch)
  - `author`: who pushed each commit
  - `mr`, `owner`: the merge request (none, open, merged) and the machine its marker names
  - `req`, `opt`: each commit's required and other check results
  - `approved`: whether the approval protection requires is present
- **Per machine:** `lease` (the tip it expects, its last push) and `checked` (the commit it saw green).
- **History variables:** `seen` (commits a run saw with every check green and approved) and `gated` (whether protection held at the moment the forge accepted the merge).
- **Atomic:** a lease push (`Push`: compare-and-swap on `tip`), opening the merge request (`Open`: refused while one is open), and a merge (`Merge`: refused if `tip` moved, or if `ForgeGate`, the forge's protection check, fails: required checks on the tip not green or approval missing).
- **Not atomic:** reading CI and the approval and then merging (`SeeGreen`, then `Merge`), and every check a run makes before it pushes. Another machine or a person can push, re-run a check or withdraw an approval in between.

### Invariants

| Invariant | Meaning | Named by requirement |
|---|---|---|
| `OneClaim` | At most one machine is shipping the change, and it is the one the merge request's marker names | ship-claim: The claim is the branch and its merge request |
| `NoLostPush` | No run's push replaces a commit another machine or a person pushed | ship-claim: Losing the claim race |
| `MergedGreen` | Only a commit a run saw with every check green and approved is merged (checks may change after that read) | forge-shipping: Merging in merge mode |
| `MergedGated` | When the forge accepted the merge, the merged commit's required checks were green and the approval held | forge-shipping: Protection enforced by the forge |
| `TypeOK` | Variables keep their types | (type check) |

### Bounds

`Machines = {m1, m2}`, `MaxCommits = 4`.
- Two machines are enough for the push race and the "merge request already open" race.
- Four commits allow: both machines' first pushes, a person's push between a run seeing green and merging, and one fix round, in one behaviour.
- Re-runs and approval changes are unbounded; the state space stays finite because each check result and the approval take three or two values.
- `Finished` lets TLC stop once the commit bound is used up, so the bound is not reported as a deadlock.

TLC (tla2tools 1.7.4, `scripts/tla-check.sh`) explores 4 605 050 distinct states in about 10 seconds.
- `ship_broken_force` is rejected after 6 states: `m1` pushes commit 1, `m2` pushes commit 2 over it.
- `ship_broken_unpinned` is rejected: a run sees its commit green, a person pushes, the person's commit passes the forge's required checks, and the unpinned merge merges a commit no run saw green.
- `ship_broken_bypass` is rejected in 9 states: `m1` pushes and claims, its checks pass and a reviewer approves, `m1` sees it all green, the reviewer withdraws the approval, and the merge goes through because the token bypasses protection.
