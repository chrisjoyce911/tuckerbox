# Design

## Context

See proposal.md for why. What exists:
- `add-core-runner` (archived): runs, worktrees, the change lock and slot locks, `ship = "local"`, rounds, `status --json`. Its design lists this change as "Planned changes" item 2: REST APIs, not `gh` or `glab`; poll CI in code; call agents only to fix; merge when green; archive; a merge request as the cross-machine claim.
- `add-herdr-board` (active): run history, dependencies, approval, `board --json` and the Herdr board mod. This change adds to its board and history.
- The manual ship workflow people follow today: push, open the merge request ready (not draft), follow CI and review for the *current head*, fix and push, retry nothing blindly, never merge around a failing check, archive in the same merge request, clean up only what is safely merged. This design automates that loop. It borrows no organisation-specific labels or reviewers.

## Goals / Non-Goals

**Goals:**
- A run can go from its last step to an open, green merge request, or to a merge, with no person in the loop unless something needs one.
- No machine overwrites another's work or a person's, and two machines never both ship one change.
- Nothing merges unless the forge enforced its branch protection (required checks and approvals) as it accepted the merge, and Tuckerbox saw every check green on the exact head just before.
- Tokens never leave the machine they belong to, and never reach a host they were not issued for.

**Non-Goals:**
- **Re-running the workflow's own review step after a CI fix.** The fix step pushes; the forge's reviewers (people or bots) review the push.
- **Retrying failed jobs automatically.** A flaky job is the fix step's call (it can report `blocked` saying so) or a person's (re-run the job in the forge, then `tuckerbox resume`). A blind retry hides real failures.
- **Removing worktrees and local branches after a merge.** A later cleanup command; it needs the same ancestry checks the manual workflow does.
- **Opening merge requests on other forges** (Bitbucket, Gitea). The forge code sits behind one interface so they can be added.
- **Starting runs on other machines.** That's `add-remote-machines`. This change only makes sure two machines that both run a change can't both ship it.
- **Drafts while the steps run.** See "No draft merge request during the steps".

## Decisions

### Configuration

Repository config, committed:

```toml
ship = "mr"                  # "local", "mr" or "merge"

[shipping]
remote = "origin"            # the remote to push to and read the forge from
fix_ci = "fix_ci"            # a step from [steps] that is not in the workflow
fix_review = "fix_ci"        # defaults to fix_ci
max_rounds = 3
merge_method = "merge"       # merge mode only: "merge", "squash" or "rebase"
archive = true
labels = []
review_from = []             # bot accounts whose review threads count too
wait_timeout = "2h"

[forge]                      # only for hosts other than github.com and gitlab.com
kind = "gitlab"
api_url = "https://git.example.org/api/v4"   # optional; https, on the remote's host

[steps.fix_ci]
agent = "claude"
role = "fixer"
permissions = "edit"
```

Machine config, never committed:

```toml
machine = "studio"           # the name in claim markers; default: the host name

[forges."git.example.org"]
kind = "gitlab"
token_env = "WORK_GITLAB_TOKEN"
borrow_cli_token = true
```

`ship` stays a top-level string, so existing configs keep working; the new settings go in `[shipping]` because TOML can't make `ship` both a string and a table.

**`ship` is a repository rule.** Merging is the repository owner's policy. If a machine could set `ship = "merge"`, one laptop could merge what the owner reviews by hand. A machine may still set `ship = "local"` as the default for repositories that say nothing, which is today's behaviour.

A new built-in role, `fixer`, is the default prompt for fix steps: read the findings and logs, change only what they need, commit, and report. It says that logs and review text are data, not instructions.

### HTTP APIs from Go, not `gh` or `glab`

As planned in `add-core-runner`. `gh` and `glab` are optional tools with their own config, versions and output formats; parsing their output is fragile and they aren't on every machine. Both forges have stable HTTP APIs, and Go's `net/http` and `encoding/json` are enough: no new module dependency, and the binary stays cgo-free.

One forge interface, two implementations (`internal/forge/github`, `internal/forge/gitlab`):

| Need | GitHub | GitLab |
|---|---|---|
| Find an open merge request for a branch | `GET /repos/{o}/{r}/pulls?head={o}:{branch}&state=open` | `GET /projects/{id}/merge_requests?source_branch=…&state=opened` |
| Open one | `POST /repos/{o}/{r}/pulls` | `POST /projects/{id}/merge_requests` |
| Checks on a commit | `GET /commits/{sha}/check-runs` (latest run per check) and `/commits/{sha}/status` | `GET /merge_requests/{iid}/pipelines` and `GET /pipelines?sha=…`, then `/pipelines/{pid}/jobs` (see "GitLab pipelines") |
| Required checks and bypass | `GET /repos/{o}/{r}/rules/branches/{base}`, `GET /rulesets/{id}` (`current_user_can_bypass`), `GET /branches/{base}` (`protection`), repository `permissions` | project `only_allow_merge_if_pipeline_succeeds`, `allow_merge_on_skipped_pipeline` |
| Failed job log | `GET /actions/jobs/{id}/logs` | `GET /projects/{id}/jobs/{jid}/trace` |
| Review threads | GraphQL `pullRequest.reviewThreads { isResolved comments }` | `GET /merge_requests/{iid}/discussions` (`resolvable`, `resolved`) |
| Reply in a thread | GraphQL `addPullRequestReviewThreadReply` | `POST /discussions/{did}/notes` |
| Mergeable | `mergeable_state == "clean"` | `detailed_merge_status == "mergeable"` |
| Merge, pinned | `PUT /pulls/{n}/merge` with `sha` and `merge_method` | `PUT /merge_requests/{iid}/merge` with `sha` (and `squash`); the method is the project's `merge_method` |
| Who may review | review's `authorCanPushToRepository`, else `GET /collaborators/{user}/permission` is `admin` or `write` | `GET /projects/{id}/members/all/{user_id}` with `access_level` ≥ 30 (Developer) |
| Tuckerbox's own account | `GET /user` | `GET /user` |

GitHub's REST API does not say whether a review thread is resolved; only its GraphQL API does. So the GitHub side uses one GraphQL query and one mutation for threads, over the same HTTP client and token. Everything else is REST.

GitLab project ids are the URL-encoded path (`team%2Ftools%2Fapp`), so subgroups need no lookup.

Forge detection reuses the board's remote normalisation (scheme, `user@` and `.git` removed, SSH aliases through `ssh -G`).

### Tokens

- **Order:** `token_env` for the host, then the forge's usual variables for its public host only (`GH_TOKEN`, then `GITHUB_TOKEN`, for `github.com`; `GITLAB_TOKEN` for `gitlab.com`), then a borrowed CLI token if allowed. `glab` reads `GITLAB_TOKEN` for any host; Tuckerbox doesn't, so a token for one GitLab can't be sent to another by accident.
- **Repository config can't name a host for a token.** `[forge] api_url` must be `https` and on the remote's host (and its port, if the remote names one). Otherwise a cloned repository could point the API at its own server, or at plain HTTP, and receive your token.
- **Redirects never carry the token elsewhere.** API and log requests use one client whose `CheckRedirect` refuses any redirect to a non-`https` URL, and removes `Authorization`, `PRIVATE-TOKEN` and `Cookie` from any redirect to another host or port. Go's default client keeps headers it does not know as sensitive (such as `PRIVATE-TOKEN`) across hosts, and keeps `Authorization` on a same-host scheme change, so the default is not enough. GitHub's job logs redirect to a signed URL on another host; that request goes without the token, which the signed URL doesn't need. Tests use `httptest.NewTLSServer`.
- **Borrowing is read-only and off by default.** `gh auth token --hostname <host>` prints the token `gh` stores, from its config file or the system keyring. `glab config get token --host <host>` does the same for `glab`. Tuckerbox runs only these commands, with a 5-second timeout, reads standard output, and never logs it. It's off by default because reading another tool's credentials should be the person's choice. `doctor` mentions when a borrowable token exists.
- **Pushing uses git's credentials** (SSH keys or a credential helper), exactly as when a person pushes. The API token is only for the API.
- **Agents never get it.** Tuckerbox doesn't add the token to an agent's environment. Variables already in the person's environment are inherited as today; that is the person's setup.
- **Masking:** before saving any forge text (logs, comments), every token Tuckerbox holds is replaced with `***`.

### After the last step

```
last step done
  → state shipping
  → openspec archive <change> --yes; openspec validate --all --strict; commit "Archive <change>"
  → commit anything left uncommitted (as local mode does)
  → refuse if the branch has no commits beyond the base
  → claim (see below): lease push, open merge request with marker
  → loop: poll → green? → mr: ready / merge: mergeable? merge pinned → merged
                → red / review threads? → fix step → push → poll
```

### Archive in the merge request, before the merge

Preferred, and chosen. The archive commit is reviewed with the work, CI checks the archived specs (`openspec validate --all --strict`), and once the merge request merges the change is done: no second merge request, no window where the work is on the base but the change is still "active" and could be picked up again. It also matches what the owner already does by hand.

Archiving after the merge would need another push to the base branch or another merge request, both of which this design avoids. `archive = false` is there for repositories that archive in another way.

The fix step's brief points at the archived folder (`openspec/changes/archive/<date>-<change>/`), since the change has moved.

### No draft merge request during the steps

Decided against, though `add-core-runner` floated it as the claim. Reasons:
- GitHub refuses a pull request with no commits beyond the base, so the claim couldn't be made before the first step commits.
- Every push during the steps would start CI, which costs runner time on work nobody should review yet.
- The steps' commits may be amended or rebased, which would need force pushes.

The cost: two machines can both *work* on a change before one ships it. `tuckerbox run` checks the forge first and refuses a change claimed elsewhere, and the board shows runs on every machine, so this needs two people to start the same change at nearly the same time. The ship-time claim then makes sure only one of them ships. A draft claim can be added later without changing the ship-time rules.

The merge request opens ready, not draft: the content is finished, and waiting for CI doesn't make it unfinished.

### The claim

Two forge operations are atomic, and the claim uses only those:
1. **A push with `--force-with-lease=<branch>:<expected>`** is a compare-and-swap on the branch tip. The first push expects no branch (`<branch>:` with an empty value). Each later push expects the commit this run last pushed, kept in `run.json`.
2. **Opening a merge request** is refused by both forges while another is open for the same source branch (GitHub 422, GitLab 409).

So:
- Two machines racing to push: one creates the branch, the other's lease fails. It is blocked and pushes nothing.
- The winner opens the merge request with its marker. If it crashed after the push, `resume` sees the tip is still its own push and opens it then.
- A person pushing to the branch moves the tip. The run sees the head is no longer its own push on its next poll and blocks, rather than build on, or overwrite, commits it didn't make. `resume` adopts the person's commits if they sit on top of the run's last push.
- A merged or closed merge request is no claim. A leftover remote branch with no open merge request is never deleted or overwritten by Tuckerbox: a person decides.

Why the lease and not a plain push: a plain push after "check that nobody claimed it" is check-then-act. Between the check and the push, the other machine pushes, and the plain push either fails (fast-forward rules) or, with `--force`, destroys the other machine's commits. `formal/ship_broken_force.tla` shows TLC finding exactly this.

The marker (`<!-- tuckerbox: change=… machine=… run=… -->`) is how other machines, `tuckerbox run`'s early check and the board tell whose claim an open merge request is, without access to the claimant's run state. A merge request with no marker (opened by a person) is treated as someone else's claim.

### CI judged for the exact head

The run judges only the commit it last pushed. A green pipeline for an earlier commit says nothing about the head. On GitHub the head's check runs (the latest run of each check, since a re-run adds a new run on the same commit) and commit statuses must all be done and passing (`success`, `neutral`, `skipped`); on GitLab the selected pipeline (below) must be `success`, ignoring `allow_failure` jobs. Required checks from branch rules that haven't reported yet keep CI `pending`, not `green`. No check at all within 5 minutes of the push is `none`: `mr` mode treats it as green (the repository has no CI, and a person merges), `merge` mode refuses to merge.

The merge call carries the head commit seen green (`sha`). If anything was pushed in between, the forge refuses, and nothing unchecked merges. `formal/ship_broken_unpinned.tla` shows the merge without the pin merging an unchecked commit.

### GitLab pipelines

A project can run branch pipelines (on the pushed commit), merge request pipelines (on the pushed commit, `source = merge_request_event`) and merged-results pipelines (on a temporary merge commit of the source into the target, which is on neither branch). A lookup by `sha` alone misses the last kind, so Tuckerbox reads `GET /merge_requests/{iid}/pipelines` first and falls back to `GET /pipelines?sha=<head>&ref=<branch>`:
- A merge request pipeline counts if its `sha` is the head.
- A merged-results pipeline (its `sha` is not the head) counts only if `GET /repository/commits/{sha}` lists the head among its `parent_ids`. Its other parent is the target commit it tested against, kept with the CI result.
- The newest merge request pipeline that counts decides; only if there is none does the newest branch pipeline for the head. When a project runs both for the head, the merge request one wins, since it is the one GitLab's "pipelines must succeed" checks.
- A pipeline whose source commit can't be confirmed is ignored (fail closed): CI stays `pending`, and then `none`.

### What the forge enforces at merge time

Reading CI and approvals, then calling merge, is not atomic. Between the two a check can be re-run on the same commit (the SHA pin still matches) and an approval can be withdrawn. Only the forge's own check, made as it accepts the merge, is atomic, and it covers what branch protection requires and nothing else. So the guarantee is split, and `formal/ship.tla` models both halves (`ReqRerun`, `OptRerun` and `Approval` act at any time):
- **Required checks and required approvals:** enforced by the forge at merge time (`MergedGated`). This holds only if the forge enforces them for Tuckerbox's token.
- **Every other check:** Tuckerbox saw it green on the merged commit before merging (`MergedGreen`). A re-run of a non-required check after that read can't stop the merge. Repositories that care make the check required.

`formal/ship_broken_bypass.tla` shows the gap: with a token that bypasses protection, a run sees everything green and approved, a reviewer withdraws the approval (or a required check is re-run), and the merge goes through.

So `merge` mode checks, before each merge attempt, that the forge will enforce protection for this token, and blocks with the reason otherwise. It never changes the forge's settings to make this so.
- **GitHub:** the base must have at least one required status check, from a ruleset (`GET /rules/branches/{base}`, readable with read access) or classic protection (`protection.required_status_checks` on `GET /branches/{base}`). Every ruleset that applies must report `current_user_can_bypass: "never"`. If the repository's `permissions.admin` is true for the token, classic protection must have `enforce_admins` enabled (readable with that admin token). The merge call has no option that forces protection on a bypassing token, so a bypassing token is refused, not trusted to behave.
- **GitLab:** the project must have `only_allow_merge_if_pipeline_succeeds = true` and `allow_merge_on_skipped_pipeline = false`. Required approvals are enforced by GitLab at merge for every role; the merge call has no bypass option, and Tuckerbox never edits approval rules.
- **Residual:** classic GitHub protection can let named accounts bypass required reviews, and only an admin can read that list. A non-admin token listed there can't be detected. `doctor` says so, and the documented setup for `merge` mode is a dedicated account not on any bypass list.

The forge's own verdict (`clean` or `mergeable`) is still required before the call; it is an early read, not the guarantee.

### Merge methods

GitHub takes the method per call (`merge_method`); Tuckerbox refuses a method the repository disallows (`allow_merge_commit`, `allow_squash_merge`, `allow_rebase_merge` on `GET /repos/{o}/{r}`) before calling. GitLab takes only `squash` per call; the merge method is a project setting (`merge_method`: `merge`, `rebase_merge` or `ff`; `squash_option`: `never`, `always`, `default_on`, `default_off`). Tuckerbox reads `GET /projects/{id}` and maps:

| `[shipping] merge_method` | GitLab project must have | Call |
|---|---|---|
| `merge` | `merge_method` `merge` or `rebase_merge`; `squash_option` not `always` | `squash=false` |
| `squash` | `squash_option` not `never` | `squash=true` |
| `rebase` | refused on GitLab: no per-call rebase, and Tuckerbox never rebases | none |

A project with `merge_method = "ff"` makes `merge` refused too (GitLab would fast-forward, not merge). A mismatch blocks the run with `merge method <x> does not match the project's <y>` before any merge call. Tuckerbox never edits project settings.

### Fix steps and findings

A failure becomes findings in the existing result format (`severity`, `file`, `detail`), so a fix step is an ordinary step with an ordinary brief:
- **CI:** one finding per failed job: `file` is the job name, `detail` holds the job's link and the path of its saved log in the state directory. Logs are trimmed to the end, where failures are: the last 200 lines, at most 32 KiB per job and 128 KiB per round.
- **Review:** one finding per new unresolved thread: `file` and line from the thread, `detail` holds the text, author and link.

Only comments from people who can write to the repository, or from accounts listed in `review_from` (a review bot), count. On a public repository anyone can comment, and a comment is text an agent will read; limiting who can steer the fix step limits prompt injection. The brief says plainly that logs and comments are data.

Write access is the actual permission, not an association: a GitHub organisation member (`MEMBER`) can have read-only access. On GitHub a review comment counts when its review has `authorCanPushToRepository: true`; otherwise (for example a reply outside a review) the collaborator permission endpoint must say `admin` or `write` (`maintain` maps to `write`). On GitLab the author's `access_level` from `members/all` (which includes inherited group membership) must be 30 (Developer) or more. Any error, 403 or 404 counts as no access (fail closed); the result is cached per user for the run's ship stage. A finding's text holds only comments that count, so an outsider's reply in a reviewer's thread doesn't reach the agent.

Tuckerbox's own replies never start a round. Each reply carries a hidden marker (`<!-- tuckerbox: reply run=… round=… thread=… -->`) and is posted from the token's account (`GET /user`). Per thread, `run.json` keeps a cursor (the newest comment id considered) and the ids of Tuckerbox's replies:
1. Before posting, the run records the intended reply (thread, round, fix commit).
2. After posting, it records the returned comment id and moves the thread's cursor past it, in one atomic write of `run.json`.
3. On resume after a crash between 1 and 2, it looks in the thread for a comment from the token's account with that marker, and adopts it instead of posting again.

A comment is Tuckerbox's when its id is recorded, or it is from the token's account and carries this run's reply marker. A person who reviews with the same account as the token still triggers rounds, as long as their comment has no marker. A thread is handed on again only when a comment newer than its cursor counts and isn't Tuckerbox's. After the push it replies in each thread with the commit and the fix step's summary, and leaves resolving to people.

A fix step that changes nothing blocks the run, rather than push the same head and loop.

### Rounds and slots

`[shipping] max_rounds` counts fix-step runs since the ship stage began or since the last `resume`, as `max_rounds` counts rejections in the workflow. The run's round number (in `run.json`) still only grows, so the stale-result rule from `formal/runs.tla` keeps holding for fix steps.

Waiting for CI can take a long time and needs no agent, so a shipping run frees its parallel slot while it waits and queues for one only to run a fix step. It keeps its change lock the whole time, so nothing else runs or edits the change. `formal/runs.tla` already allows a run to take and free a slot more than once while holding its change lock (`WithinLimit` holds whenever a slot is held).

### Polling

Per run, one request cycle (merge request, head checks, threads) per interval:
- 30 seconds, doubling while nothing changes, up to 5 minutes; back to 30 seconds after any change.
- `Retry-After` is obeyed. Under 10% of the rate limit left (`X-RateLimit-Remaining` on GitHub, `RateLimit-Remaining` on GitLab), no request until the reset.
- GitHub requests send `If-None-Match` with the last `ETag`; a `304` doesn't count against the limit.
- Network errors and 5xx back off with full jitter, up to 5 minutes.
- If nothing changes for `wait_timeout` (2 hours by default), the run blocks with what it waited for (`waiting for CI timed out`, `waiting for approval timed out`). `resume` waits again.

Ten shipping runs at the slowest steady rate make about 360 requests an hour, well under GitHub's 5000.

### States and status

Two new run states:
- `shipping`: past the last step, working on the forge (archiving, pushing, waiting, fixing).
- `merged`: merged by Tuckerbox or seen merged by someone else. Final.

`ready` keeps its meaning, "nothing more for Tuckerbox to do", which in `mr` mode is "green, waiting for a person to merge". `resume` on such a run goes back to `shipping` to pick up later review comments; `run` still refuses a ready run.

Status JSON keeps its `version`. New fields (`mr`, `ci`, `ship_round`) are additions; the new state values are allowed by the run-status delta, and the board mod already falls back for unknown states.

### History

New event kinds, appended by the run's own process like every other event: `archived`, `pushed`, `mr_opened`, `ci_finished`, `review_findings`, `review_replied`, `merged`, `branch_deleted`. A lost claim, a moved branch, a closed merge request or a timeout are `blocked` events with their reason, as today.

### Board

A Shipping column sits between the last step and Ready, only in `mr` and `merge` repositories. A fix step shows in Shipping (not its own column, since it isn't a workflow step), with the step name on the card. Blocked and failed shipping runs stay in Shipping with the red flag, as blocked runs stay in their step's column. Merged runs go to Done straight away, before the local checkout fetches the archive.

Cards gain `mr` and `ci` fields and the ship round, and the mod shows a link and a CI badge. The mod adds Open merge request and, on Ready cards in `mr` mode, Resume. It gets no merge action: merging is the repository's rule, carried out by `merge` mode or by people in the forge.

`board --json` reads the merge request and CI fields from `run.json`, which the shipping run keeps up to date. The board never calls the forge itself, so opening a board costs no API requests.

### How this fits `add-herdr-board`

`run-history` and `board-data` are in `add-herdr-board`, which isn't archived yet, so this change doesn't write deltas against them. Their additions are in `ship-board`:
- the history events add to `run-history`'s event list
- the Shipping column and the card fields add to `board-data`'s columns and card summary
- the board actions add to `herdr-board`'s actions

`add-herdr-board` must be archived first. When this change is archived, `ship-board`'s requirements become a spec of their own; folding them into those three is a later tidy-up.

### Model and code

`formal/ship.tla` assumes three things of the code, each a single forge call:

| Model | Code (under `internal/`) |
|---|---|
| `Push`: compare-and-swap on the tip | `ship.push`: `git push --force-with-lease=refs/heads/<b>:<lease> <remote> refs/heads/<b>:refs/heads/<b>`, lease from `run.json` |
| `Open`: one open merge request per branch | `forge.OpenMR`; on 409/422, `forge.FindMR` and compare the marker |
| `SeeGreen`, `MergeGuard`: merge pinned to the commit seen green | `ship.ciState(head)`, then `forge.Merge(sha)` |
| `ForgeGate`, `Protected`: the forge enforces required checks and approval at merge | `ship.mergeGate()` (the preflight in "What the forge enforces at merge time") before each `forge.Merge` |
| `ReqRerun`, `OptRerun`, `Approval` | anything on the forge that re-runs a check on the same commit or changes an approval |
| `PersonPush` | anything else that moves the branch; detected as `head != lease` on poll |

`OneClaim`, `NoLostPush`, `MergedGreen` and `MergedGated` are named by ship-claim and forge-shipping requirements. `scripts/tla-check.sh` already runs every `formal/*.cfg` with its broken variants, so it needs no change.

## Risks / Trade-offs

- **[A person merges a merge request that Tuckerbox is still fixing]** → The next poll sees it merged, and the run becomes `merged` and stops. Nothing is pushed after a merge.
- **[GitHub's `mergeable_state` is computed lazily and may be `unknown`]** → Treated as "not yet"; poll again. Never merged on `unknown`.
- **[Required checks can't always be read]** → Branch rules and `GET /branches/{base}` need only read access; full classic protection needs admin. If no required check can be found, `merge` mode refuses (see "What the forge enforces at merge time"); `mr` mode doesn't need it.
- **[A non-required check is re-run between the read and the merge]** → The merge can go through; Tuckerbox saw that check green on the merged commit, which is all it promises for checks protection doesn't require. Make the check required to have the forge enforce it.
- **[A GitHub bypass list Tuckerbox can't read]** → See the residual in "What the forge enforces at merge time": use a dedicated account for `merge` mode.
- **[A fix agent works from untrusted text]** → Only reviewers with write access and named bots count; logs are marked as data; agents keep their permission profiles; the token is not in their environment.
- **[Archive date]** → `openspec archive` stamps the date it runs, which can be days before a person merges in `mr` mode. Accepted: the date says when the change was finished.
- **[Archive conflicts]** → Two changes archived in parallel can both touch `openspec/specs/<cap>/spec.md`. The second merge request then conflicts with the base. In `merge` mode Tuckerbox merges the base into the branch and blocks on a conflict; in `mr` mode the forge shows the conflict to people.
- **[A long wait holds the change lock]** → It must, so no edit lands while shipping. `stop` ends the wait; `resume` continues it. The slot is free meanwhile.
- **[`gh` or `glab` output changes]** → Only their "print the token" commands are used, which are meant for scripts. A failure means no borrowed token, and `doctor` says so.
- **[Rate limits shared with the person]** → A personal token's limit is shared with the person's other tools. The 10% floor leaves them room.
- **[The early claim check is advisory]** → Between `tuckerbox run`'s check and the ship stage, another machine may claim the change. The ship-time claim decides; the early check only saves agent time in the common case.

## Migration Plan

- Nothing changes for repositories with `ship = "local"`, the default.
- A repository opts in by setting `ship = "mr"` or `"merge"`, adding `[shipping]` and a fix step, and each machine that ships it needs a token: `tuckerbox doctor` shows what is missing.
- This repository moves to `ship = "mr"` once the change is built, since its owner merges pull requests.
- Rollback: set `ship = "local"`. Open merge requests and branches stay on the forge for people to finish.

## Open Questions

- The exact token permissions to document for fine-grained GitHub tokens and GitLab project tokens (contents, pull requests, checks and actions read; `api` scope on GitLab). Settled when `doctor`'s access check is written; it doesn't change the specs.
