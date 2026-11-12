# Design

## Context

Tuckerbox is a new repository with no code yet. The capabilities we want come from four Herdr mods reviewed on 2026-11-12:
- **Factory:** per-repo workflows, a stage/result contract, review loops.
- **Muster:** file-based gates, a PR watch loop, a dashboard covering every machine.
- **Kelpie:** worktree per task, a program per role.
- **Shepherdr:** placement across machines.

None of them has OpenSpec support, GitLab, setup kept in the repo, or a way to run without Herdr. We borrow ideas, not code.

The first users run it on a handful of Macs, some headless with nobody logged in, and a Linux server. The tool must also build and run on Windows.

## Goals / Non-Goals

**Goals:**
- One static Go binary with no cgo, built and tested on macOS, Linux and Windows.
- A repository fully describes how its changes are run, in `.tuckerbox/config.toml`.
- Several changes run in parallel on one machine, each isolated in its own worktree.
- Any agent CLI can be used per step. Permission bypass is never granted by the repo.
- Status is machine-readable from the start, so dashboards are thin.

**Non-Goals (this change):**
- Pushing, MRs/PRs, CI watching and merging (change 2).
- Running on other machines (change 3).
- Herdr tabs and a Herdr panel (change 4).
- Several agents at once on one change (change 5).
- A long-running daemon, a web UI or a central server.

## Decisions

### Planned changes after this one
1. `add-core-runner` (this change): local runs, config, agents, status.
2. `add-forge-shipping`: `ship = "mr"` and `"merge"`. GitHub and GitLab through their REST APIs, not `gh` or `glab`. Push, open the MR/PR, poll CI and review feedback in code, call agents only to fix, merge when green, archive the change. A draft MR/PR is the cross-machine claim on a change.
3. `add-remote-machines`: `machines = [...]` in config. Start runs on another machine over SSH, or with `hc run --on` when herdr-core is present. `tuckerbox status --all-machines`.
4. `add-herdr-board`: run history, change dependencies, `board --json`, and a Herdr board mod (Kelpie-style kanban of changes, merged across machines). Written 2026-11-12.
4b. `add-herdr-terminals`: a terminal backend that runs agents as herdr-core terminals labelled as Herdr tabs, so they can be watched and typed into from the board.
5. `add-parallel-steps`: steps that fan out over groups in tasks.md in separate worktrees and join before review.

### Config format: TOML in `.tuckerbox/`
TOML is easy to write by hand and has a mature Go parser (`github.com/BurntSushi/toml`). Role prompts that override the built-in defaults go beside the config, as `.tuckerbox/roles/<role>.md`.

Alternatives considered:
- Extending `openspec/config.yaml`: rejected, because OpenSpec owns that schema.
- A config file kept only on each machine (as in Muster and Kelpie): rejected, because the repo must define its own workflow.

Example:

```toml
max_parallel = 2
ship = "local"            # change 2 adds "mr" and "merge"

[workflow]
steps = ["plan", "build", "review"]

[steps.plan]
agent = "claude"
role = "planner"

[steps.build]
agent = "codex"
role = "builder"
permissions = "edit"      # a profile the machine must define and allow

[steps.review]
agent = "claude"
role = "reviewer"
on_reject = "build"
max_rounds = 3
```

### Permissions belong to the machine
A repo you clone must not be able to make your agents run without prompts. Machine config defines named profiles with flags and the repo remotes allowed to use them. A repo can only refer to a profile by name. This follows Factory's refusal of repo `args`, but keeps the ability to grant.

### Run state lives in the worktree's git directory
Run state goes in `$(git rev-parse --git-dir)/tuckerbox/`, which is per worktree:
- `run.json`
- one result file per step and round
- one final-message file per step and round, for agents that write it (codex `-o`)
- briefs
- logs

It is never committed, needs no `.gitignore` entry, and is removed with the worktree. A machine index lists the known worktrees, so `status` can find runs across repos without scanning disks. It is `index.json` in the user state directory:
- `$XDG_STATE_HOME/tuckerbox/` when `XDG_STATE_HOME` is an absolute path, on any OS
- otherwise `~/.local/state/tuckerbox/` on macOS and Linux (not `~/Library` on macOS, to match the config directory)
- otherwise `%LocalAppData%\tuckerbox\` on Windows

Each run's engine adds its worktree when it starts, and drops entries whose worktree is gone.

### Concurrency through OS file locks
There is no daemon. Each active run is one `tuckerbox` process that holds two locks:
- an exclusive lock on `<git-dir>/tuckerbox/run.lock`, for the change
- one of `max_parallel` slot locks in the main repository's git common dir: `tuckerbox/slots/<n>.lock`

Locks are `flock` on Unix and `LockFileEx` on Windows, through `github.com/gofrs/flock`. The OS releases them if the process dies, so a crash never leaves a change stuck. A run waiting for a slot is `queued`. `tuckerbox run --all` starts one process per change, which queue on the slot locks.

The engine is the only writer of `run.json`, and writes it atomically (a temporary file, then a rename). The engine also writes each step's result file, from the agent's final message; an agent may write it too. `stop` signals the run process, which writes the state change itself.

How the pieces fit, as built:
- **Order of locks.** A run takes the change lock first, then waits for a slot, as in the model. Runs waiting for a slot hold a ticket in `<common-dir>/tuckerbox/queue/`, and only the oldest live ticket may take one, so queued runs start in the order requested. A ticket is live while its change lock is held, so a dead run's ticket is dropped by the next waiter. `run` starts its changes one at a time and waits until each holds its lock and has joined the queue.
- **Worktree creation** happens before the change lock exists (the lock lives in the worktree's git directory). It is serialised by `<common-dir>/tuckerbox/worktree.lock`, so two processes racing for one change make the worktree once, and then race only for the change lock. A new worktree is refused, and nothing is made, when the change is not on the base branch, since its agents could not read it.
- **Rounds.** A run's round is stored in `run.json` and only grows. It goes up when a rejection loops back, and also on `resume` and `rerun`, so nothing an earlier attempt's agent writes can match. `max_rounds` counts the rejecting step's rejections since the last `resume` or `rerun`.
- **`run` on a change that already has a run** continues it from its current step, in its worktree, unless it is `ready`; then `run` refuses and points to `rerun`. `run --all` skips ready and active changes.
- **Paused runs end their process.** A `blocked`, `stopped`, `failed` or `ready` run records its state and then frees its slot and change lock. `resume` and `rerun` start a new process.
- **Stop** creates `<git-dir>/tuckerbox/stop` and, on Unix, sends SIGTERM. The engine polls for the file, which is how a detached run on Windows is stopped gracefully. If the run has not stopped within 30 seconds, `stop` kills it and its agent; `run.json` then still says `running`.
- **A crashed run** leaves `run.json` saying `queued` or `running` with its change lock free. `status` shows it as `stopped` with the reason "the run process ended without recording a final state", and `resume` treats it as stopped. Nothing rewrites the file except the next engine.
- **Detached processes.** `run`, `resume` and `rerun` start `tuckerbox engine` for each run: a new session with no controlling terminal on Unix, a detached process with its own process group on Windows. It writes its log to `<git-dir>/tuckerbox/run.log`. `--foreground` runs one change in the calling process instead, and Ctrl-C stops it.

### Locks and the model

`formal/runs.tla` assumes three things of the code: the change lock is taken atomically, at most `MaxParallel` slots are held, and a result is accepted only for the current round. Each action and invariant maps to code as follows (paths under `internal/`):

| Model | Code |
|---|---|
| `Pick(p, c)`: take a free change lock in one step | `runs.TryChangeLock`: one `flock.TryLock` on `<git-dir>/tuckerbox/run.lock`. It retries a few times, each retry atomic, and never checks first and locks second. Called by `Engine.Run` before anything else is written. |
| `TakeSlot(p)`: take a slot while fewer than `MaxParallel` are taken | `runs.TrySlot`: try-lock `<common-dir>/tuckerbox/slots/<n>.lock` for `n < max_parallel`, through `runs.WaitSlot`. The queue (`runs.Queue`) only decides who may try first, so it cannot add a run beyond the limit. |
| `AgentWrites(p)` | The agent ends with the result as its final message, which the engine writes to `<git-dir>/tuckerbox/results/<step>-<round>.json`; the agent may also write that file itself, the path in its brief. |
| `LateWrite(c)`: an earlier round's agent overwrites the file | Not prevented, only tolerated. Agents of earlier rounds are stopped before the next round starts, and each round has its own file, but the round check below is what makes it safe. |
| `AcceptGuard(c)`: `result[c] = round[c]` | `agent.ReadResult` (file) and `agent.MessageResult` (final message) with `agent.Expect{Change, Step, Round}` accept only an exact match; anything else is a `StaleError` and is ignored. `agent.Run` also moves a leftover result file at the round's path aside, and removes a leftover final-message file, before the agent starts. |
| `Accept(p)`: rejection starts the next round, otherwise release | `Engine.steps`: `rejected` does `Round++` and goes to `on_reject`; `done` goes to the next step (the model has one step per round; more steps under the same lock change nothing it checks). The last `done`, `blocked`, `failed`, `stopped` and the round limit all end in `Engine.finish`. |
| `round`, written only by the lock holder | `Run.Round` in `run.json`, written only by `Engine.save` while the change lock is held. It only grows, also on `resume` and `rerun`. |
| `Release(p)`: free the change lock and the slot | `Engine.finish` writes the final state, then the deferred `Slot.Release` and `ChangeLock.Release` run, in that order. Between the two the process holds a change lock and no slot, which the model's `locked` state allows. If the process dies, the OS frees both. |
| `OneRunPerChange` | Only the holder of the change lock runs steps. Tested by `TestChangeAlreadyRunning` (`internal/runs`) and, with two real processes, `TestTwoProcessesRaceForOneChange` (`internal/cli`). |
| `WithinLimit` | Steps run only while a slot lock is held. Tested by `TestSlots`, `TestQueueWithOneSlot`, `TestMoreChangesThanSlots` and `TestRunAllQueuesWithOneSlot`. |
| `StaleNeverAccepted` | `TestStaleResultIgnored` (`internal/runs`), and the stale-result tests in `internal/agent`. |

Two things the code does that the model leaves out, neither of which breaks an invariant:
- **`status`, `stop` and the queue probe a change lock** with `runs.Active`, which takes a free lock for an instant and releases it. That is a `Pick` followed by a release without running. A run starting at that moment fails its try-lock, so `TryChangeLock` retries for about 200 ms before it reports the change as running.
- **The slot count comes from each process's config.** The limit holds for one `max_parallel` value. If the config changes while runs are active, processes using the old and new values share the slot files, and the larger value is the effective limit until the old runs end.

### Agents run headless in this change
Agents run headless as child processes:
- Claude: `claude -p <brief> --output-format stream-json`
- Codex: `codex exec --json <brief>`

Output goes to log files. Interactive terminals that can be watched in Herdr come in change 4, behind a `terminal` backend interface, so the engine doesn't change.

### Agents report in their final message
An agent reports by ending with its result JSON as its final message (a fenced `json` block or the bare object). The engine reads it and writes the step's result file itself.

Why: the result file lives in the git directory, outside the worktree, and a sandboxed agent cannot write there. The first self-run showed it: `codex exec --sandbox workspace-write --add-dir <repo .git>` reviewed the change but could not write its result, because Codex keeps `.git` read-only even in writable roots. A reviewer should also be able to run read-only (`--sandbox read-only`) and still report. So agents need no write access outside their worktree.

Where the final message comes from:
- **Claude:** the `result` field of the last `{"type":"result"}` event in its stream-json output.
- **Codex:** `-o <file>` (`--output-last-message`) in the built-in template, pointing at `<git-dir>/tuckerbox/messages/<step>-<round>.txt`. The Codex CLI writes that file itself, outside the sandbox it runs the model's commands in. Without `-o`, the last `agent_message` item of `--json` output.
- **Custom agents:** the `{lastmessage}` placeholder for a file the CLI writes the final message to, or else its standard output as plain text; the last result JSON in it is used.

A result file the agent writes itself is still accepted. If both are valid and differ, the file wins and the difference is logged: a file is a deliberate write, while a final message may be cut short or reworded. The round and step check applies to both, so `StaleNeverAccepted` holds either way.

### OpenSpec through its CLI
`openspec list --json` and `openspec validate <id>` decide which changes exist and are valid. Tuckerbox doesn't parse spec files itself, beyond passing their paths in the brief.

### Command name
The binary is `tuckerbox`. A short alias is decided before release (see Risks).

## Risks / Trade-offs

- **Headless agents can't be watched live until change 4.** `tuckerbox logs --follow` is the only view meanwhile.
- **Headless machines:** Claude's sign-in sits in the login keychain, which is locked while nobody is logged in. Runs there need a keychain-free token, such as `claude setup-token`; this is untested. This change doesn't solve it, but `tuckerbox doctor` should report it.
- **Agent CLI flags change between releases.** Built-in agent templates are kept small and can be overridden in machine config.
- **One process per active run** is simple and survives crashes, but `stop` and `resume` must find the process through the lock and a pid file. This works on all three OSes but needs Windows testing.
- **Stopping agents on Windows** kills only the agent process. Processes it started keep running until Tuckerbox puts agents in a job object. On macOS and Linux the agent runs in its own process group, and stop signals the whole group.
- **Short alias:** `tb` and `tbx` may clash with existing tools. To be checked before the first release.
