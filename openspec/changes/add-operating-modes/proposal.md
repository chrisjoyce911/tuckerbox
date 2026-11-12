# Proposal

## Why

Tuckerbox runs a repository's changes on the machine where someone typed
`tuckerbox run`. That suits one person at one desk. It doesn't suit an owner
with a desktop, a laptop and a box in a cupboard, who wants approved changes
to go to whichever machine is free and has the right agents signed in. Nor
does it suit several people working in one repository, who need to know who
has which change, and whose approval counts.

`add-core-runner` planned `add-remote-machines` for the first case. Muster
and Shepherdr show the shape: a panel of machines, work placed by what each
machine has. Both need a background watcher; we don't want a daemon. Both
cases also share one hard problem: two machines picking up the same change
at once. That needs a claim that survives crashes and is safe under races.

## What Changes

- **`mode` in repository config:** `"solo"` (default, today's behaviour),
  `"fleet"` or `"team"`. A repository rule, like `ship`.
- **Machine inventory is machine config:** `[machines.<name>]` with how to
  reach each machine (`ssh = "<destination>"` or `hc = "<herdr-core
  machine>"`), and `fleet_capacity` for how many fleet runs this machine
  takes. `tuckerbox machine --json` reports a machine's free capacity,
  installed agents, sign-in state and checkout. `tuckerbox doctor` checks
  agent sign-in.
- **Fleet mode** (one owner, several machines; absorbs `add-remote-machines`):
  - A claim per change in one git ref on the remote, `refs/tuckerbox/fleet`,
    written only by `git push --force-with-lease`. `tuckerbox run` claims
    before it makes a worktree, and refuses a change claimed elsewhere.
  - The run's own process renews the claim. A stopped or blocked run parks
    it, so it doesn't expire. A claim not renewed for `[fleet] lease`
    (10 minutes) expires. A person may then take the change over with
    `tuckerbox run --take-over`.
  - Every push of the change's branch carries a lease on the fleet ref in
    the same atomic push. A take-over of a pushed branch moves its tip with
    a take-over commit, so the old holder's fix pushes and pinned merge are
    refused. The new holder rewrites the merge request's marker by epoch
    and carries on shipping.
  - `[fleet] max_runs` limits claims across all machines.
  - A ready run's next task scope, or the rest of the change
    (`add-task-scope`), reuses its claim: the claim becomes active again
    only if `max_runs` has room, checked in the same compare-and-swap.
    Dispatch continues such runs on their holder's machine, so autopilot
    finishes changes group by group.
  - `tuckerbox dispatch` places approved, unclaimed changes in one pass: by
    the change's preferred machines (`add-change-settings`), free capacity,
    installed agents and sign-in. It starts runs over SSH or with
    `hc run --on`. Run it by hand, from the board, or from your own
    scheduler. No daemon.
  - `tuckerbox status --all-machines`.
  - TLA+ model `formal/fleet.tla` with two broken variants TLC rejects.
- **Team mode** (several people):
  - The forge claim from `add-forge-shipping` is the lock, taken at the
    start instead of the end: an empty start commit, the same lease push,
    and a draft merge request with the claim marker. It is marked ready at
    the ship stage.
  - A run's next task scope, or the rest of the change, reuses its branch
    and draft, with no new start commit.
  - Cards show who holds each change (forge login and machine).
  - `[team] approvers` lists the forge logins whose approval counts, read
    only from a base branch the forge confirms is protected.
  - Requires `ship = "mr"` or `"merge"`.
- **Board:** the mode, claim state (active, parked, expired, ready), holder
  and owner on cards; Dispatch and Take over actions.

## Capabilities

### New Capabilities

- `operating-modes`: the `mode` key and what each mode requires and allows.
- `machine-inventory`: the machine list, capacity, `tuckerbox machine`,
  sign-in checks, reaching other machines and `status --all-machines`.
- `fleet-claims`: the fleet ref, claiming, renewing, parking, expiry,
  take-over (also of a branch being shipped), fenced pushes and merges,
  and the fleet limit.
- `fleet-placement`: `tuckerbox dispatch`: choosing changes and machines and
  starting runs remotely.
- `team-claims`: the early forge claim, owners on cards and approvers.
- `modes-board`: claims, holders and owners on the board, and its actions.
  It adds to `board-data` and `herdr-board` once `add-herdr-board` is
  archived.

### Modified Capabilities

- `repo-config`: `mode`, `[fleet]` limits and `[team]` are repository-only;
  the machine list and capacity are machine-only.

`ship-claim` and `forge-shipping` (in `add-forge-shipping`, not archived)
are referenced, not changed. Team mode takes the same claim earlier; the
one narrowing (a draft merge request at start) is in `team-claims` (see
design.md).

## Impact

- Depends on `add-forge-shipping` (the claim, forge access, the machine
  name) and `add-change-settings` (preferred machines, effective agents).
- New ref on the remote in fleet mode: `refs/tuckerbox/fleet`. Pushing it
  needs only git's own push rights. CI does not run on it.
- New commands: `tuckerbox dispatch`, `tuckerbox machine`, `tuckerbox claim`
  (`show`, `release`); new flags `run --for`, `run --take-over`,
  `status --all-machines`.
- New machine keys `[machines.<name>]` and `fleet_capacity`. New
  repository keys `mode`, `[fleet]`, `[team]`.
- Remote starts run `tuckerbox` on other machines over SSH or herdr-core,
  so `tuckerbox` must be on the PATH there.
- Solo repositories see no change.
