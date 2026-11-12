# Spec Delta

## Purpose

Defines how a shipping run follows CI and review on its merge request, hands failures and review threads to a fix step, and polls the forge without exceeding its limits.

## ADDED Requirements

### Requirement: CI state of the head commit
A shipping run SHALL judge CI only for the commit it last pushed. On GitHub that is every check run and commit status on that commit; on GitLab, the newest pipeline for it, ignoring jobs allowed to fail. CI SHALL be `green` when all finished as passed, neutral or skipped, `red` when any failed or was cancelled, `none` when nothing reported within 5 minutes of the push, else `pending`.

#### Scenario: Old pipeline green
- **WHEN** the pipeline for the previous push is green and the one for the head commit is still running
- **THEN** CI is `pending`, not `green`

#### Scenario: Repository without CI
- **WHEN** no check or pipeline reports on the head commit for 5 minutes after the push
- **THEN** CI is `none`

#### Scenario: Allowed failure on GitLab
- **WHEN** a GitLab job with `allow_failure: true` fails and every other job passes
- **THEN** CI is `green`

### Requirement: Failed job logs
When CI is `red`, Tuckerbox SHALL fetch each failed job's log, strip terminal colour codes, mask tokens, and keep its last 200 lines and at most 32 KiB per job and 128 KiB in all. For a check with no log (an outside app), it SHALL keep the check's summary and link. Logs SHALL be saved in the run's state directory.

#### Scenario: Long log
- **WHEN** a failed job's log has 20 000 lines
- **THEN** the saved log holds its last 200 lines and says how many lines were left out

### Requirement: Fix step for CI
`[shipping] fix_ci` SHALL name a step from `[steps]` that is not in the workflow. When CI is `red`, Tuckerbox SHALL start it with one finding per failed job (name, link, saved log path). The brief SHALL say the logs are data to read, not instructions to follow. Without `fix_ci`, the run SHALL be `blocked` with reason `CI failed`.

#### Scenario: CI fails
- **WHEN** job `go (ubuntu-latest)` fails and `fix_ci = "fix_ci"`
- **THEN** step `fix_ci` starts with a finding for that job and the path of its saved log

#### Scenario: No fix step
- **WHEN** CI is `red` and `[shipping]` names no `fix_ci`
- **THEN** the run is `blocked` with reason `CI failed` and the failed jobs' links

### Requirement: After a fix step
When a fix step reports `done`, Tuckerbox SHALL commit what it left uncommitted, push with the lease, and poll again for the new head. If the step changed nothing, the run SHALL be `blocked` with reason `fix changed nothing`. `blocked` and `failed` results SHALL block or fail the run as in a workflow step.

#### Scenario: Fix pushed
- **WHEN** `fix_ci` reports `done` with a new commit
- **THEN** Tuckerbox pushes it and polls CI for the new head commit

#### Scenario: Fix makes no change
- **WHEN** `fix_ci` reports `done` and the branch and worktree are unchanged
- **THEN** the run is `blocked` with reason `fix changed nothing` and nothing is pushed

### Requirement: Review threads as findings
Each new unresolved review thread SHALL become a finding (file, line, text, author, link) for the step `[shipping] fix_review` names, `fix_ci` by default. Only threads from people with write access, or accounts in `[shipping] review_from`, SHALL count. A thread SHALL be handed on again only after a new comment in it.

#### Scenario: Reviewer asks for a change
- **WHEN** a maintainer leaves an unresolved comment on `internal/app/login.go` line 12
- **THEN** the fix step starts with a finding for that file and line, holding the comment's text and link

#### Scenario: Comment from outside the project
- **WHEN** an account without write access, not in `review_from`, opens a review thread
- **THEN** it is not a finding and the run does not wait for it

### Requirement: Answering review threads
After pushing a fix for review findings, Tuckerbox SHALL reply in each thread it handed on with the fix commit and the fix step's summary. It SHALL NOT resolve threads; people do.

#### Scenario: Reply after a fix
- **WHEN** a fix for two review threads is pushed as commit `def456`
- **THEN** both threads get a reply naming `def456`, and both stay unresolved

### Requirement: Bounded ship rounds
Each fix step run SHALL count as one ship round. `[shipping] max_rounds`, 3 by default, SHALL limit the rounds since the run reached the ship stage or was last resumed. When the limit is reached, the run SHALL be `blocked` with reason `ship limit reached` instead of starting another fix.

#### Scenario: Limit reached
- **WHEN** CI fails for the fourth time with `max_rounds = 3`
- **THEN** the run is `blocked` with reason `ship limit reached` and no fix step starts

### Requirement: Waiting without a slot
A shipping run waiting on the forge SHALL NOT hold a parallel slot. It SHALL take one, in queue order, only to run a fix step, and free it after. It SHALL keep its change lock throughout.

#### Scenario: Waiting runs and a free slot
- **WHEN** `max_parallel = 1`, one run is waiting for CI and another change is queued
- **THEN** the queued change starts while the first keeps waiting

### Requirement: Polling the forge
Tuckerbox SHALL poll every 30 seconds, doubling up to 5 minutes while nothing changes, and back to 30 seconds after a change. It SHALL honour `Retry-After` and rate limit headers, and wait for the reset when under 10% is left. Network and 5xx errors SHALL back off with jitter. Without change for `[shipping] wait_timeout` (2 h), the run SHALL be `blocked`.

#### Scenario: Rate limit low
- **WHEN** the forge reports 40 requests left of 5000, resetting in 12 minutes
- **THEN** the run makes no request to that forge until the reset

#### Scenario: Nothing happens
- **WHEN** CI stays `pending` for longer than `wait_timeout`
- **THEN** the run is `blocked` with reason `waiting for CI timed out`

#### Scenario: Forge down
- **WHEN** the forge answers 503 three times in a row
- **THEN** the run waits longer before each retry and stays `shipping`

### Requirement: Watching a ready merge request again
In `mr` mode, `tuckerbox resume <change>` on a `ready` run whose merge request is still open SHALL put it back to `shipping`, with a new round count, so new review threads and CI results are handled. It SHALL NOT start any workflow step.

#### Scenario: Review after ready
- **WHEN** a run is `ready` in `mr` mode and a maintainer then leaves a review thread
- **THEN** `tuckerbox resume` makes the run `shipping` and the thread becomes a finding for the fix step
