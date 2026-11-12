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
- briefs
- logs

It is never committed, needs no `.gitignore` entry, and is removed with the worktree. A machine index at `<user state dir>/tuckerbox/index.json` lists the known worktrees, so `status` can find runs across repos without scanning disks.

### Concurrency through OS file locks
There is no daemon. Each active run is one `tuckerbox` process that holds two locks:
- an exclusive lock on `<git-dir>/tuckerbox/run.lock`, for the change
- one of `max_parallel` slot locks in the main repository's git common dir: `tuckerbox/slots/<n>.lock`

Locks are `flock` on Unix and `LockFileEx` on Windows, through `github.com/gofrs/flock`. The OS releases them if the process dies, so a crash never leaves a change stuck. A run waiting for a slot is `queued`. `tuckerbox run --all` starts one process per change, which queue on the slot locks.

The engine is the only writer of `run.json`. Agents write only their own result file. `stop` signals the run process, which writes the state change itself.

### Agents run headless in this change
Agents run headless as child processes:
- Claude: `claude -p <brief> --output-format stream-json`
- Codex: `codex exec --json <brief>`

Output goes to log files. Interactive terminals that can be watched in Herdr come in change 4, behind a `terminal` backend interface, so the engine doesn't change.

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
