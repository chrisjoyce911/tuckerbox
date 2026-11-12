# Model

## Model

Shipping adds state shared between machines: the change's branch and merge request on the forge. Several run processes on different machines, and people, write it, with no lock they share. The only atomic primitives are two forge operations, so the design's claim is modelled.

### Files

| File | Role |
|---|---|
| `formal/ship.tla`, `formal/ship.cfg` | Model; TLC must pass |
| `formal/ship_broken_force.tla`, `.cfg` | Broken variant: a run pushes without `--force-with-lease` (a plain or forced push after checking nobody claimed the change); TLC must reject (`NoLostPush`) |
| `formal/ship_broken_unpinned.tla`, `.cfg` | Broken variant: the merge is not pinned to the commit seen green; TLC must reject (`MergedGreen`) |

### Actors and shared state

- **Machines (`Machines`):** one shipping run each, for the same change. A run pushes, opens the merge request, waits for green, merges, and pushes again after each fix round.
- **A person (`PersonPush`):** may push to the branch at any time, with no lease.
- **CI (`CI`):** reports `green` or `red` on each pushed commit, once.
- **Shared state on the forge:**
  - `tip`: the branch's tip commit (0: no branch)
  - `author`: who pushed each commit
  - `mr`, `owner`: the merge request (none, open, merged) and the machine its marker names
  - `ci`: each commit's CI result
- **Per machine:** `lease` (the tip it expects, its last push) and `checked` (the commit it saw green).
- **Atomic:** a lease push (`Push`: compare-and-swap on `tip`), opening the merge request (`Open`: refused while one is open), and a pinned merge (`Merge`: refused if `tip` moved).
- **Not atomic:** reading CI and then merging (`SeeGreen`, then `Merge`), and every check a run makes before it pushes. Another machine or a person can act in between.

### Invariants

| Invariant | Meaning | Named by requirement |
|---|---|---|
| `OneClaim` | At most one machine is shipping the change, and it is the one the merge request's marker names | ship-claim: The claim is the branch and its merge request |
| `NoLostPush` | No run's push replaces a commit another machine or a person pushed | ship-claim: Losing the claim race |
| `MergedGreen` | The forge only merges a commit whose CI was green | forge-shipping: Merging in merge mode |
| `TypeOK` | Variables keep their types | (type check) |

### Bounds

`Machines = {m1, m2}`, `MaxCommits = 4`.
- Two machines are enough for the push race and the "merge request already open" race.
- Four commits allow: both machines' first pushes, a person's push between a run seeing green and merging, and one fix round, in one behaviour.
- `Finished` lets TLC stop once the commit bound is used up, so the bound is not reported as a deadlock.

TLC (tla2tools 1.7.4, `scripts/tla-check.sh`) explores 8 297 distinct states in under a second.
- `ship_broken_force` is rejected after 6 states: `m1` pushes commit 1, `m2` pushes commit 2 over it.
- `ship_broken_unpinned` is rejected after a few thousand states: a run sees its commit green, a person pushes, and the unpinned merge merges the person's unchecked commit.
