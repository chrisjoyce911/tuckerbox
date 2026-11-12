# Design

## Context

See proposal.md. `change-priority` writes `priority` into the checkout's sidecar and never commits. `approval-publish` already publishes approvals as commits on `refs/tuckerbox/approvals` with a lease, offline queue and team-mode exception. `add-forge-shipping` (built, not archived) polls the merge request's CI and review threads, and in `merge` mode merges the base into a branch the forge says is behind, blocking on any conflict. `add-autopilot` (built, not archived) reads run state and acts on what it saw under the change lock; it never resumes a blocked or stopped run. A run ends only by reaching `ready`/`merged` through its own steps.

## Goals / Non-Goals

**Goals:**
- A priority move is visible on every machine at once and never dirties the checkout.
- No run stays blocked for a change that is already shipped.
- A conflicting merge request is resolved or handed to a person, never waited on.
- Every commit on `main` gets a completed CI run.

**Non-Goals:**
- Rebasing change branches.
- Moving other sidecar keys off the base branch.
- Closing runs whose change was merely deleted (not archived) on the base: that needs a person.

## Decisions

### Priority on its own ref, same publish routine

Options: the approvals ref (one ref, but priority churn would contend with approvals and mix two meanings in one history), committing the sidecar (needs a merge each move), or a ref of its own. Chosen: `refs/tuckerbox/priority`, holding one file `priority.toml`:

```toml
[priority]
add-login = 1000
add-users = 1500
```

A move fetches the ref, plans the move against the order it implies, writes the new `priority.toml` as one commit (parent: the fetched one) and pushes with `--force-with-lease`. On refusal it fetches again and **re-plans the move** (change, target position) on the new order, rather than re-applying old values, so two concurrent moves both take effect in the order the ref accepts them. The fetch, lease, retry (5, jitter), offline queue and `doctor` check are the approvals code, factored into one routine for both refs.

Reading: an entry on the ref wins; without one, a `priority` in the change's sidecar (as read today) counts, so existing values keep working. Gaps and renumbering keep `change-priority`'s rules, now within one file. The sidecar lock no longer applies to moves outside team mode.

Offline: as approvals. The commit stays on the local ref and counts on this machine; the state directory lists unpublished moves (change and target), which are re-planned onto the remote's version on the next move or board fetch; the board shows `not published`.

Team mode writes the sidecar as today, because team mode reads repository state from the protected base. Priority isn't a trust question, but one rule for both is simpler and was asked for.

Migration: `tuckerbox check` warns about uncommitted `priority` lines in the checkout's sidecars. The first move after upgrade publishes the full current order (including uncommitted checkout values) to the ref, then says the checkout lines can be discarded.

### Closing runs

A run can close two ways:

- **Merged elsewhere** (`merged`, reason `archived on <base>` or `merge request merged`): the change's folder is absent from `openspec/changes/` and present as `openspec/changes/archive/*-<change>/` at the fetched default branch, or the run's merge request is merged. Autopilot checks this each pass; `status` and the board fetch check it at most once a minute (same cache as the approvals fetch). A change folder gone without an archive entry is reported, not closed.
- **By a person**: `tuckerbox close <change> [--reason <text>]` marks the run `closed`.

Either way it takes the change's run lock and closes only a run with no agent running (`queued`, `blocked`, `stopped`, `failed`, `ready`, crashed); a running one is closed once it stops or blocks. A queued run gives up its queue place. Worktree and branch are kept (as after a forge merge); closing writes a history event. A closed run is not resumed by autopilot; `tuckerbox run` starts a new run if the change is still active. `stop` on a run with no agent marks it `stopped` and exits 0 instead of `no active run`, so the board's Stop works on a blocked card too.

`status` lists `merged` and `closed` runs only with `--all`; the board shows no run for them. `status --json` adds them only with `--all` (a field-compatible addition to the format).

Model: `formal/autopilot.tla` gains `archived`, `ArchiveElsewhere`, `PersonClose` and `AutoClose`. Invariant `ClosesOnlyIdleArchived`: autopilot closes only a run with no agent whose change is archived. `autopilot_broken_closeunseen` closes on what autopilot saw, without checking the run again under the change lock, and TLC must reject it. Existing invariants also cover that autopilot never starts or resumes an archived or person-closed change.

### Conflicts while shipping

Each poll reads the merge request's merge state before CI: GitHub `mergeable` (`null` means not computed yet: poll again) and `mergeable_state = "dirty"`; GitLab `has_conflicts` or `detailed_merge_status = "conflict"`. A conflicting merge request is handled at once in both modes, whatever CI says (GitHub reports none).

Round: take a parallel slot (as a fix step does), fetch the base, `git merge --no-ff <base>` in the worktree. If clean, commit. If not, leave the merge in progress and run the fix step with a brief listing the conflicting files and both sides' commits; it must resolve and commit the merge. Tuckerbox then checks there are no unmerged paths, no conflict markers in the touched files (`git diff --check` against the base), and that `HEAD` has the base as a parent; any failure aborts the merge and blocks. It pushes with the lease (as `Push safety`), frees the slot and waits for CI again.

Each round (clean or resolved) counts as one ship round against `max_rounds`; reaching the limit blocks with `ship limit reached`. A fixer that can't resolve blocks with `merge conflict: <files>`, which is `needs_you`. Merge, not rebase: keeps `Push safety`'s lease simple and review threads anchored. This narrows `Branch behind the base`, whose conflict now goes to the fixer instead of blocking. `formal/ship.tla` needs no change: the merge commit is one more lease push, which the model already covers.

### CI concurrency on main

`ci.yml` today: `group: ci-${{ github.ref }}`, `cancel-in-progress` only for pull requests. With `cancel-in-progress: false` GitHub still keeps just one pending run per group and cancels older pending ones, so a burst of merges leaves middle commits, and sometimes the tip, without a completed run. Change the group to `ci-${{ github.event_name == 'pull_request' && github.ref || github.sha }}`: pull requests keep one group per branch and cancel superseded runs; every push to `main` gets its own group and is never cancelled. Cost: runner minutes for bursts of merges, which is acceptable.

## Risks / Trade-offs

- **[A merge by hand that wasn't the full change]** → Closing needs the archive entry on the default branch, not just merged code; a merged merge request alone closes only the run that opened it.
- **[Fixer resolves conflicts wrongly]** → CI runs again on the result, and the AI review sees the merge commit; mechanical checks catch leftover markers.
- **[Priority ref contention]** → Moves are rare and re-planning is cheap.
- **[Two sources of priority during migration]** → The ref wins; `check` warns about checkout values.

## Migration Plan

Existing sidecar priorities keep counting until a move publishes the order. Old runs blocked on shipped changes close on the first autopilot pass or `status` after upgrade. Rollback: an older Tuckerbox ignores the priority ref; it may not recognise `merged` or `closed` runs written by this one.
