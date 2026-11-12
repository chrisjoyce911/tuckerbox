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
- Nothing merges that the forge's own protection, or a green check on the exact head, would not allow.
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
api_url = "https://git.example.org/api/v4"   # optional; must be on the remote's host

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
| Checks on a commit | `GET /commits/{sha}/check-runs` and `/commits/{sha}/status` | `GET /projects/{id}/pipelines?sha=…`, then `/pipelines/{pid}/jobs` |
| Required checks | `GET /repos/{o}/{r}/rules/branches/{base}` (readable without admin) | `detailed_merge_status` on the merge request |
| Failed job log | `GET /actions/jobs/{id}/logs` | `GET /projects/{id}/jobs/{jid}/trace` |
| Review threads | GraphQL `pullRequest.reviewThreads { isResolved comments }` | `GET /merge_requests/{iid}/discussions` (`resolvable`, `resolved`) |
| Reply in a thread | GraphQL `addPullRequestReviewThreadReply` | `POST /discussions/{did}/notes` |
| Mergeable | `mergeable_state == "clean"` | `detailed_merge_status == "mergeable"` |
| Merge, pinned | `PUT /pulls/{n}/merge` with `sha` and `merge_method` | `PUT /merge_requests/{iid}/merge` with `sha` (and `squash`) |
| Who may review | `author_association` in `OWNER`, `MEMBER`, `COLLABORATOR` | project member with Developer access or more |

GitHub's REST API does not say whether a review thread is resolved; only its GraphQL API does. So the GitHub side uses one GraphQL query and one mutation for threads, over the same HTTP client and token. Everything else is REST.

GitLab project ids are the URL-encoded path (`team%2Ftools%2Fapp`), so subgroups need no lookup.

Forge detection reuses the board's remote normalisation (scheme, `user@` and `.git` removed, SSH aliases through `ssh -G`).

### Tokens

- **Order:** `token_env` for the host, then the forge's usual variables for its public host only (`GH_TOKEN`, then `GITHUB_TOKEN`, for `github.com`; `GITLAB_TOKEN` for `gitlab.com`), then a borrowed CLI token if allowed. `glab` reads `GITLAB_TOKEN` for any host; Tuckerbox doesn't, so a token for one GitLab can't be sent to another by accident.
- **Repository config can't name a host for a token.** `[forge] api_url` must be on the remote's host. Otherwise a cloned repository could point the API at its own server and receive your token.
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

The run judges only the commit it last pushed. A green pipeline for an earlier commit says nothing about the head. On GitHub the head's check runs and commit statuses must all be done and passing (`success`, `neutral`, `skipped`); on GitLab the newest pipeline for the head must be `success`, ignoring `allow_failure` jobs. Required checks from branch rules that haven't reported yet keep CI `pending`, not `green`. No check at all within 5 minutes of the push is `none`: `mr` mode treats it as green (the repository has no CI, and a person merges), `merge` mode refuses to merge.

The merge call carries the head commit seen green (`sha`). If anything was pushed in between, the forge refuses, and nothing unchecked merges. `formal/ship_broken_unpinned.tla` shows the merge without the pin merging an unchecked commit.

Tuckerbox also requires the forge's own verdict (`clean` or `mergeable`) and never uses admin bypass, so required approvals, code owners and protection rules still apply, even with an admin's token.

### Fix steps and findings

A failure becomes findings in the existing result format (`severity`, `file`, `detail`), so a fix step is an ordinary step with an ordinary brief:
- **CI:** one finding per failed job: `file` is the job name, `detail` holds the job's link and the path of its saved log in the state directory. Logs are trimmed to the end, where failures are: the last 200 lines, at most 32 KiB per job and 128 KiB per round.
- **Review:** one finding per new unresolved thread: `file` and line from the thread, `detail` holds the text, author and link.

Only threads from people with write access, or from accounts listed in `review_from` (a review bot), count. On a public repository anyone can comment, and a comment is text an agent will read; limiting who can steer the fix step limits prompt injection. The brief says plainly that logs and comments are data.

Tuckerbox remembers each thread's last comment id it handed on (in `run.json`), so a thread comes back only when someone adds to it. After the push it replies in each thread with the commit and the fix step's summary, and leaves resolving to people.

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
| `PersonPush` | anything else that moves the branch; detected as `head != lease` on poll |

`OneClaim`, `NoLostPush` and `MergedGreen` are named by ship-claim and forge-shipping requirements. `scripts/tla-check.sh` already runs every `formal/*.cfg` with its broken variants, so it needs no change.

## Risks / Trade-offs

- **[A person merges a merge request that Tuckerbox is still fixing]** → The next poll sees it merged, and the run becomes `merged` and stops. Nothing is pushed after a merge.
- **[GitHub's `mergeable_state` is computed lazily and may be `unknown`]** → Treated as "not yet"; poll again. Never merged on `unknown`.
- **[Required checks can't always be read]** → Branch rules need only read access, but classic branch protection needs admin. If neither can be read, Tuckerbox still requires every reported check green and the forge's `clean` verdict, which includes required checks. The forge stays the last word.
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
