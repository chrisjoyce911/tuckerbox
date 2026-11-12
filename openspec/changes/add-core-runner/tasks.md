# Tasks

## 1. Repository and build

- [x] 1.1 Go module, `cmd/tuckerbox`, `internal/` layout, Apache-2.0 licence, README and AGENTS.md
- [x] 1.2 CI that runs `go vet`, `go test -race` and builds on macOS, Linux and Windows, plus `tla:check` for `formal/`
- [x] 1.3 `tuckerbox version`, `tuckerbox doctor` (reports missing `git`, `openspec` and agent CLIs; agent sign-in is reported as "sign-in not checked" until a check without side effects exists)

## 2. Config (repo-config)

- [x] 2.1 Load and merge machine and repo `config.toml` with repo precedence; reject unknown keys
- [x] 2.2 Machine-only permission profiles with allowed remotes; reject raw agent args in repo config
- [x] 2.3 `tuckerbox check`: validate steps, loops, agents and profiles, with file and key in every message
- [x] 2.4 `tuckerbox init`: write a commented starter `.tuckerbox/config.toml`
- [x] 2.5 Tests for each repo-config scenario

## 3. Runs (change-runs)

- [x] 3.1 Change selection through `openspec list --json` and `openspec validate`
- [x] 3.2 Worktree and branch creation and reuse; never reset, clean or stash
- [x] 3.3 Change lock and slot locks with gofrs/flock; queued state; one process per run
- [x] 3.4 Step engine: done, rejected, blocked and failed transitions, `on_reject`, `max_rounds`
- [x] 3.5 Local ship mode: final commit on the branch, state `ready`
- [x] 3.6 `stop`, `resume` and `rerun`, with pid file and signals (Unix and Windows)
- [x] 3.7 Tests for each change-runs scenario, including two processes racing for one change

## 4. Agents (agent-launch)

- [x] 4.1 Agent command templates; built-in `claude` and `codex`; custom agents from config
- [x] 4.2 Brief writer with built-in role prompts (planner, builder, reviewer) and `.tuckerbox/roles/` overrides
- [x] 4.3 Result file parsing with round and step checks; stale and malformed files ignored and logged
- [x] 4.4 Per-step logs; `tuckerbox logs <change> [step] [--follow]`
- [x] 4.5 Tests with a fake agent binary for each agent-launch scenario

## 5. Status (run-status)

- [x] 5.1 Machine index of known worktrees
- [x] 5.2 `tuckerbox status` table in local time; `--json` with `version`, RFC 3339 timestamps
- [x] 5.3 Tests for each run-status scenario

## 6. Model

- [ ] 6.1 Run TLC on `formal/runs` (must pass) and both broken variants (must be rejected): `/opsx:tlc` locally, `tla:check` in CI
- [x] 6.2 Check the lock implementation against the model's assumptions (atomic change lock, slot lock, round check on results)

## 7. Dogfood

- [ ] 7.1 Add `.tuckerbox/config.toml` to this repository and run a later change with `tuckerbox run`
- [ ] 7.2 Archive this change after the implementation is accepted
