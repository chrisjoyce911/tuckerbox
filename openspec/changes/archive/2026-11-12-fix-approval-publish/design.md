# Design

## Context

See proposal.md for why. `change-approval` (in `add-herdr-board`, built, not archived) stores an approval as `[approval]` in the change's `tuckerbox.toml`, a hash of the change's text. The run gate judges it in the files the run will use: the worktree, else a retained change branch, else the base branch at the fetched commit. So a new approval counts only once committed and merged to the base. `add-change-settings` adds `settings_hash`. `add-operating-modes` makes team mode read approvals only from a protected base.

## Goals / Non-Goals

**Goals:**
- Approve on the board, and every machine of the repository can run the change straight away.
- Approvals still bind to the exact text and settings approved.
- Two people approving at once never lose one of the approvals.

**Non-Goals:**
- Changing team mode's trust rule.
- Moving other sidecar keys (`title`, `depends_on`, step settings) off the base branch. They describe the change and are reviewed with it.
- Signing approvals.

## Decisions

### A dedicated ref, not the base branch

Options: commit the sidecar to the base branch (needs a merge, or a direct push that branch protection usually forbids), a branch per approval (one pull request each), the forge's own approval (team mode's question, and forge-only), or a ref outside `refs/heads/`. Chosen: `refs/tuckerbox/approvals`, as `add-operating-modes` does for fleet claims. It is not a branch, so CI doesn't run on it and branch protection doesn't apply; git's own credentials can push it; every machine already fetches from the remote.

The ref holds a commit whose tree has `approvals/<change>.toml`:

```toml
state = "approved"            # or "withdrawn"
hash = "sha256:…"             # the change's text, as change-approval defines it
settings_hash = "sha256:…"    # add-change-settings
by = "Sam Example"
at = 2026-11-12T19:40:00+10:00
```

Each approve or unapprove is one commit with the previous one as parent, so `git log refs/tuckerbox/approvals` is the approvals' history. A withdrawal is a file, not a deletion, so it can override an older `[approval]` still in a sidecar on the base.

### Pushed at once, with a lease

`approve`: take the change lock (as today), validate, compute the hashes, then:

```
git fetch <remote> +refs/tuckerbox/approvals:refs/tuckerbox/approvals
build commit = fetched tree + approvals/<change>.toml, parent = fetched
git push --force-with-lease=refs/tuckerbox/approvals:<fetched> <remote> <commit>:refs/tuckerbox/approvals
refused because the ref moved → fetch and repeat (the one file is re-applied to the new tree)
```

The lease is what keeps concurrent approvals: a plain push of a commit built from an old fetch would replace someone else's newer approval. `formal/approvals.tla` checks this (`NoLostApproval`), and `approvals_broken_plainpush` shows TLC losing one of two concurrent approvals. Retries are bounded (5, with jitter); after that the command fails and says to try again, leaving the local commit as an offline one.

### Reading

The gate and the board fetch the ref (the board at most once a minute, cached in the state directory) and read the change's file. An entry wins; with none, the old `[approval]` in `tuckerbox.toml` counts as before, so approvals already merged keep working. The hash is compared with the files the run will use, exactly as today, so an approval of text that later changes is `stale`. The gate's second judgment under the change lock fetches again. As with the base branch today, an unapprove pushed from another machine after that fetch is not seen by a run already admitted.

The card no longer depends on the checkout's sidecar, so it doesn't fall back to Proposed when the checkout changes.

### Offline

If the push fails because the remote can't be reached (not because the lease failed), the commit stays on the local ref and Tuckerbox keeps a list of unpublished changes in the state directory. On this machine the local ref counts, so the owner can approve and run on a train. The next approve, unapprove or board fetch pushes: it fetches, re-applies each unpublished file onto the remote's version, and pushes with the lease. The card shows `not published` until then. Other machines only see published approvals.

A repository with no remote keeps the ref locally; there is nothing to publish and no note.

### Team mode

Unchanged: `approve` writes `[approval]` into `tuckerbox.toml` for a reviewed merge to the protected base, and the gate reads only that base, as `add-operating-modes`' `Approvals only from the protected base` requires. A ref anyone with push access can move is not a trusted source for a team.

### How this fits `add-herdr-board`

`change-approval` isn't archived, so the rules are ADDED in `approval-publish` and narrow `Approving a change` (where the approval is written), `Withdrawing approval`, `Run gate` and `Gate at the start` (where it is read) for solo and fleet. `board-data`'s `Card approval` gains `published`; `committed` no longer applies to approvals on the ref. Folding these in is a tidy-up after archive.

## Risks / Trade-offs

- **[Anyone with push access can approve]** → True in solo and fleet, where the people with push access are the owner and the owner's machines. Team mode keeps the protected base.
- **[Ref contention]** → One push per approve or unapprove; retries are rare and cheap.
- **[A remote that refuses custom refs]** → The push fails with the remote's message; `doctor` checks that the ref can be pushed; the approval stays local and counts on this machine.
- **[Two sources of truth during migration]** → The ref wins; the sidecar is only a fallback. `tuckerbox check` lists changes approved in both places with different hashes.

## Migration Plan

Nothing to migrate: existing sidecar approvals still count. New approvals go to the ref. Rollback: an older Tuckerbox ignores the ref and reads sidecars only.
