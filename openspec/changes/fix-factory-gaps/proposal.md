# Proposal

## Why

Four gaps showed up on 2026-11-12 while autopilot ran this repository:

1. **Priority edits stay local.** A move on the board or with
   `tuckerbox priority` rewrites `tuckerbox.toml` in the main checkout and
   leaves it uncommitted (as `change-priority` says). The dirty files block
   `git pull`, and no other machine sees the new order.
2. **Runs never end when a change ships some other way.** A change merged
   by hand or archived on `main` leaves its run `blocked` in
   `tuckerbox status` and on the board for good, and `tuckerbox stop`
   refuses with `no active run`. There is no way to clear it.
3. **Ship waits forever on a conflicting pull request.** GitHub runs no
   `pull_request` checks while a pull request conflicts with its base, so
   the head has no CI and shipping polls until `wait_timeout`. Today a
   conflict only blocks in `merge` mode, and only once the forge says the
   branch is behind.
4. **Main's CI runs get cancelled.** Several merges in a row put several
   pushes to `main` in one concurrency group; GitHub keeps one pending run
   per group and cancels the others, so `main` can end up with no completed
   run for its tip.

## What Changes

- **Priority is published at once, like approvals.** In solo and fleet
  mode a move is one commit on `refs/tuckerbox/priority` (file
  `priority.toml`), pushed straight away with `--force-with-lease` and
  retried on refusal, using the same publish routine as
  `refs/tuckerbox/approvals`. Offline it counts locally, the board shows
  `not published`, and the next move or board fetch publishes it. The
  order reads the ref first, then the sidecar. Team mode keeps writing the
  sidecar. Nothing is written to the checkout.
- **Runs close when their change is gone.** Autopilot, `tuckerbox status`
  and the board fetch notice a change archived on the fetched default
  branch, or whose merge request was merged, and mark a run that has no
  agent running `merged`. New `tuckerbox close <change>` clears a run that
  has no agent running as `closed`. `stop` on a blocked or failed run no
  longer errors. Closed and merged runs leave the default status listing.
  TLA+ `formal/autopilot.tla` gains closing, with a broken variant.
- **Ship resolves conflicts.** In `mr` and `merge` mode, a merge request the
  forge reports as conflicting (GitHub `mergeable = false`, GitLab
  `has_conflicts`) is not waited on: Tuckerbox merges the base into the
  change's branch, runs the fix step to resolve the conflicts, checks no
  conflict is left, pushes with the lease and waits for CI again. Each such
  round counts against `[shipping] max_rounds`. It blocks with
  `merge conflict` (needs you) when the fixer can't resolve it.
- **No agent waits on the owner.** Tasks only a person can do are marked
  `(owner)`. Autopilot does not start a step when only owner tasks (and
  `(ci)`/`(ship)` tasks) are left: the run is blocked with `owner tasks:
  <numbers>` and the card says what is needed. Autopilot also no longer
  restarts a run that blocked or failed when nothing has changed since
  (seen 2026-11-13: an agent waited three hours on hand checks).
- **CI never cancels runs on `main`.** Pushes to the default branch get a
  concurrency group per commit; pull requests keep cancelling superseded
  runs.

## Capabilities

### New Capabilities

- `priority-publish`: where priority moves are stored and published, how
  the order reads them, offline moves, team mode.
- `run-closing`: closing runs whose change was archived or merged
  elsewhere, `tuckerbox close`, closed runs in status and on the board.
- `ship-conflicts`: detecting and resolving merge conflicts while
  shipping. Narrows `forge-shipping`'s `Branch behind the base` (in
  `add-forge-shipping`, not archived).

- `owner-tasks`: the `(owner)` task marker, no runs for owner tasks only,
  no restart of an unchanged block.

### Modified Capabilities

- `change-priority`: the order reads the priority ref; moves no longer
  write the checkout outside team mode.
- `change-runs`: `stop` on a run with no agent; `close`.

## Impact

- `internal/priority`, `internal/approval` (shared ref publish code),
  `internal/runs`, `internal/autopilot`, `internal/ship`, `internal/forge`
  (mergeable state for GitHub and GitLab), `internal/board`, `internal/cli`
  (`close`), the board mod.
- New ref `refs/tuckerbox/priority` on the remote.
- `.github/workflows/ci.yml` concurrency.
- Model: `formal/autopilot.tla` and a new broken variant.
