# Model

## Model

Fleet mode adds state several machines write with no lock they share: the claims in `refs/tuckerbox/fleet` on the git remote. Machines crash, sleep and wake up unaware, and claims expire by time. The only atomic primitive is a git push with `--force-with-lease` (with `--atomic` across several refs), so the claim protocol is modelled.

Team mode needs no new model. Its claim is `add-forge-shipping`'s, taken earlier: the empty start commit's push is `formal/ship.tla`'s first `Push`, and opening the draft is its `Open`. `OneClaim` and `NoLostPush` cover it (see design.md, "Team mode").

### Files

| File | Role |
|---|---|
| `formal/fleet.tla`, `formal/fleet.cfg` | Model; TLC must pass |
| `formal/fleet_broken_plainclaim.tla`, `.cfg` | Broken variant: a claim pushes the fleet ref without `--force-with-lease`, deciding from a stale fetch; TLC must reject (`OneHolderPerEpoch`) |
| `formal/fleet_broken_unfenced.tla`, `.cfg` | Broken variant: the ship stage's first push leases only the change's branch, not the fleet ref; TLC must reject (`FencedPush`) |

### Actors and shared state

- **Machines (`Machines`):** each fetches the fleet ref, claims or takes over one change, renews, may crash and wake (`Crash`, `Wake`), pushes the change's branch at the ship stage (`Ship`), and releases.
- **Time (`Expire`):** marks a claim expired at any moment, whatever its holder is doing. No clock is modelled, so safety can't depend on one.
- **Shared state on the remote:**
  - `version`: the fleet ref's commit, as a counter
  - `claim[c]`: holder and epoch per change
  - `pushed[c]`: who created the change's branch, and with which epoch
- **Per machine:** `seen` and `snap` (the version and claims it last fetched), `job` and `epoch` (the claim it believes it holds), `pc`.
- **Atomic:** a lease push of the fleet ref (`Claim`: compare-and-swap on `version`); the ship stage's atomic push of branch and fleet ref (`Ship`).
- **Not atomic:** fetching and then deciding to claim (`Fetch`, then `Claim`); everything a machine does between renewals.
- **Abstractions:**
  - `Renew` and `Release` act on the current ref in one step. In the code they fetch, check and lease-push, and retry on refusal, which comes to the same.
  - A parked claim is a claim whose `Expire` never fires, a subset of the modelled behaviours.
  - `Release` frees a change for a new claim, which the design forbids for `ready` claims. That allows more behaviours than the design, so the invariants still hold for it.

### Invariants

| Invariant | Meaning | Named by requirement |
|---|---|---|
| `OneHolderPerEpoch` | Each claim on a change, first or take-over, is handed to exactly one machine | fleet-claims: Claiming at run start |
| `FencedPush` | No machine pushes the change's branch after its claim was taken over | fleet-claims: Fenced first push |
| `WithinFleetLimit` | The fleet ref never holds more claims than `max_runs` | fleet-claims: Fleet limit |
| `TypeOK` | Variables keep their types | (type check) |

### Bounds

`Machines = {m1, m2}`, `Changes = {c1, c2}`, `MaxRuns = 1`, `MaxEpoch = 2`, `MaxVersion = 5`.
- Two machines are enough for both races: two claims from the same fetch, and a sleeping holder against the machine that took over.
- Two changes with `MaxRuns = 1` exercise the limit: the second change can only be claimed once the first is released.
- `MaxEpoch = 2` allows a first claim and one take-over per change.
- `MaxVersion = 5` allows a claim, a renewal, a take-over, a release and another claim in one behaviour.
- `Finished` lets TLC stop once every machine is done or the version bound is used up, so the bound is not reported as a deadlock.

TLC (tla2tools 1.7.4, `scripts/tla-check.sh`) explores 3 268 distinct states in about a second.
- `fleet_broken_plainclaim` is rejected after 5 states: `m1` and `m2` fetch, then both claim `c1` with epoch 1.
- `fleet_broken_unfenced` is rejected after 7 states: `m1` claims `c1`, its claim expires, `m2` takes it over with epoch 2, and `m1`, unaware, pushes the branch.
