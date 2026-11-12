# Model

## Model

Several people on several machines approve and unapprove changes, and each publishes by pushing the same ref with no lock they share. The only atomic primitive is a push with `--force-with-lease`, a compare-and-swap on the ref, so the publish protocol is modelled.

### Files

| File | Role |
|---|---|
| `formal/approvals.tla`, `formal/approvals.cfg` | Model; TLC must pass |
| `formal/approvals_broken_plainpush.tla`, `.cfg` | Broken variant: the commit built from the fetch is pushed without a lease (a plain or forced push); TLC must reject (`NoLostApproval`) |

### Actors and shared state

- **Machines (`Machines`):** each runs approve or unapprove for some change (`Begin`), fetches the ref (`Fetch`), and pushes a commit built from the fetched approvals with that one change set (`Push`). A refused push fetches again.
- **Shared state:** `version` (the ref's commit) and `ref` (approved or not per change).
- **Ghost:** `want`, the last value accepted for each change.
- **Atomic:** the push with its lease.
- **Not atomic:** fetching, then building and pushing; another machine can push in between.

### Invariants

| Invariant | Meaning | Named by requirement |
|---|---|---|
| `NoLostApproval` | The ref always holds, for each change, the last approve or unapprove that was accepted | approval-publish: Published at once |
| `TypeOK` | Variables keep their types | (type check) |

### Bounds

`Machines = {m1, m2}`, `Changes = {c1, c2}`, `MaxVersion = 4`.
- Two machines and two changes are enough for concurrent approvals of different changes and of the same change.
- Four versions allow two rounds of concurrent pushes, with retries.

TLC (tla2tools 1.7.4, `scripts/tla-check.sh`) explores 24 890 distinct states in a few seconds.
- `approvals_broken_plainpush` is rejected after 6 states: both machines fetch, both push, and the second push drops the first's approval.
