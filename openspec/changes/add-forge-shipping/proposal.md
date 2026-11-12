# Proposal

## Why

Today a run ends at `ready`: the work sits on a local branch, and a person
pushes it, opens the merge request, watches CI, fixes what fails, answers
review comments and archives the change. That is the slow, repetitive part
of the work, and it is the part agents are not needed for, except to fix
things. `add-core-runner` planned this as its change 2.

Shipping also adds the first state Tuckerbox shares between machines: the
change's branch and merge request on the forge. Two machines can reach the
end of the same change at once, so the claim on a change must be safe
against that race.

## What Changes

- **Ship modes:** `ship = "mr"` and `ship = "merge"` join `"local"`.
  - `mr`: push, open the merge request, follow CI and review until CI is
    green and no review thread is waiting, then stop as `ready` for people
    to merge. This repository uses it.
  - `merge`: the same, then merge when the forge says the merge request is
    mergeable and every check on its head is green.
  - The mode is a repository rule: machine config may not set `mr` or
    `merge`.
- **GitHub and GitLab** (gitlab.com and self-hosted) through their HTTP
  APIs, called from Go. No `gh` or `glab` is needed. The forge is detected
  from the remote URL. Repository config can name it for other hosts.
- **Tokens stay on the machine:** from machine config or the usual
  environment variables (`GITHUB_TOKEN`, `GH_TOKEN`, `GITLAB_TOKEN`), and
  only for the host they belong to. With `borrow_cli_token = true`,
  Tuckerbox may read the token `gh` or `glab` already stores, read-only.
  Repository config can never hold a token. `git push` keeps using git's own
  credentials.
- **After the last step:** archive the change in the branch (so the archive
  is part of the merge request), push the branch, open the merge request
  ready for review, with its title and body from the change. No draft while
  the steps run.
- **CI and review loop:** poll the checks for the head commit; on failure,
  fetch the failed jobs' logs, trimmed, and give them to a fix step the
  repository names. Review threads from people with write access (and
  named bots) become findings for the same loop. Each fix is pushed and
  polled again, up to `max_rounds`.
- **Cross-machine claim:** a push with `--force-with-lease` and the forge's
  "one open merge request per branch" rule decide which machine ships a
  change. The other machine stops as `blocked` and pushes nothing. TLA+
  model `formal/ship.tla`, with two broken variants TLC rejects.
- **Safety:** never push the base branch, never force-push except with a
  lease on the change's branch, never merge with a failing, pending or
  missing check, always merge pinned to the commit seen green, and poll
  with backoff inside the forge's rate limits.
- **Status, history and board:** new run states `shipping` and `merged`;
  the merge request link, CI state and ship round in `status` and on
  board cards; a Shipping column between the last step and Ready; history
  events for archive, push, merge request, CI, review and merge.

## Capabilities

### New Capabilities

- `forge-access`: finding the forge from the remote, reaching its API, and
  where tokens come from.
- `forge-shipping`: the `mr` and `merge` modes from the last step to
  `ready` or `merged`: archive, push, merge request, merge rule and the
  safety rules.
- `ship-claim`: how one machine claims a change on the forge, and how the
  others back off.
- `ship-feedback`: following CI and review on the merge request and
  handing failures to a fix step, with polling limits.
- `ship-board`: shipping on board cards and in run history. It adds to
  `board-data` and `run-history` once `add-herdr-board` is archived.

### Modified Capabilities

- `repo-config`: forge tokens are machine-only; `ship = "mr"` and
  `"merge"` are repository-only.
- `run-status`: new states `shipping` and `merged`, and the merge request,
  CI state and ship round in the listing.

`change-runs`' `Local ship mode` stays as it is. The new modes are in
`forge-shipping`.

## Impact

- Depends on `add-herdr-board` (run history, the board and dependencies)
  and `add-task-scope`: a scoped run whose change isn't complete ends
  `ready` and never ships (task-scope, `Partial runs don't ship`).
  Its `board-data` and `run-history` specs are not archived yet, so this
  change keeps their additions in `ship-board` (see design.md).
- New Go packages for forge access (an HTTP client per forge, no new
  binary dependency). The binary stays cgo-free.
- New repository keys: `[shipping]` and `[forge]`. New machine keys:
  `[forges."<host>"]` with `kind`, `token_env` and `borrow_cli_token`.
- A run in `mr` or `merge` mode writes to the forge: branches, merge
  requests, replies on review threads and, in `merge` mode, merges.
- The Herdr board mod gains the Shipping column and the merge request and
  CI fields.
- This repository keeps `ship = "local"` until this change is built. Its
  rule then is `ship = "merge"`: `AGENTS.md` lets the coordinating agent
  merge once CI is green and a cross-model review has no open findings,
  and approval is the owner's only routine gate. Merge mode merges when
  the forge says the merge request is mergeable, so the cross-model review
  must be a forge rule (branch protection requiring an approving review).
  Until that rule is set, the repository uses `ship = "mr"` and the
  coordinating agent merges.
