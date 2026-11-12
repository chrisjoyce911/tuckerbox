# Tasks

## 1. Backend choice (repo-config, agent-terminals, agent-launch)

- [ ] 1.1 Parse machine-only `agent_backend` (`auto`, `terminal`, `headless`); refuse it in repository config; `doctor` reports the backend and why; document it in README
- [ ] 1.2 Test "Repository sets the backend"
- [ ] 1.3 Test "No herdr-core" (no `hc` on PATH; headless; doctor's reason)
- [ ] 1.4 Test "Terminal asked for without herdr-core" (fake `hc` whose `server status` fails; no run state)
- [ ] 1.5 Backend interface in `internal/agent`; today's `Run` becomes the headless backend unchanged
- [ ] 1.6 Test "Following a running step" and "Headless fallback" (existing headless tests pass through the interface)

## 2. Terminal backend (agent-terminals)

- [ ] 2.1 Interactive templates for `claude` and `codex`; custom `interactive` with `{turnhook}`; agents without one run headless
- [ ] 2.2 Test "Custom agent without an interactive command"
- [ ] 2.3 Terminal start under the change lock: `hc run --bg --name tb-<hash> --cwd <worktree> --no-parent --outlive-parent` with `herdr.space`, `herdr.tab` and `tuckerbox.*` labels; address in `run.json`; tested against a fake `hc` that records calls and enforces unique live names
- [ ] 2.4 Test "Agent tab in Herdr" (name, cwd and labels)
- [ ] 2.5 Adopt a live terminal whose step and round labels match; otherwise `hc kill` and wait before starting
- [ ] 2.6 Test "Leftover terminal from an older round"
- [ ] 2.7 Interactive start with the brief prompt, model, effort, profile flags and `TUCKERBOX_*` environment
- [ ] 2.8 Test "Person answers a question" (fake interactive agent that reads a line from its terminal and then ends its turn)
- [ ] 2.9 Hidden `tuckerbox agent-turn`: Claude `Stop` hook input (transcript path → last assistant message) and Codex `notify` input (`last-assistant-message`); atomic write of the last-message file; hooks passed with `--settings` and `-c notify=…`
- [ ] 2.10 Test "Turn ends with a result" (result accepted, program ended after the grace, history kept)
- [ ] 2.11 Test "Stale result in a later turn"
- [ ] 2.12 A turn without a valid result keeps the step running with agent state `waiting for you`
- [ ] 2.13 Test "Agent asks before finishing"
- [ ] 2.14 `stop`: signal the run process when it holds the lock; otherwise take the lock, `hc kill <name>`, record `stopped`
- [ ] 2.15 Test "Stop after a crash" (run process killed; agent terminal live; stop ends it)
- [ ] 2.16 Crash detection with a live terminal shows `running` and `not watched`; `resume` and autopilot's crash resume adopt it with no new round
- [ ] 2.17 Slot agent locks (`slots/<n>.agent.lock`, and for fleet slots): admission takes a slot only when both locks are free; record the slot number in `run.json`
- [ ] 2.18 `tuckerbox agent-hold --slot <n> -- <argv>` (hidden): takes the agent lock, refuses if held, runs the agent as its child, passes its exit status; the terminal runs it
- [ ] 2.19 Adoption takes the run lock of the agent's own slot from `run.json`, needing no free slot, waiting out an admission's brief hold
- [ ] 2.20 Test "Crash, then another change queued" (kill the run process; the queued change stays queued until the agent ends)
- [ ] 2.21 Test "Crash with room for another change"
- [ ] 2.22 Test "Agent ends while orphaned"
- [ ] 2.23 Test "Crash, another change admitted, then adoption" (separate processes holding real locks; never more than `max_parallel` agents)
- [ ] 2.24 Run TLC on `formal/liveslots.tla` and `liveslots_broken_runlockonly` (`/opsx:tlc`, `scripts/tla-check.sh` in CI)
- [ ] 2.25 Test "Resume after the run process died"
- [ ] 2.26 `logs` through `hc read` (and following) while the terminal lives; append its history to the step log at the end
- [ ] 2.27 Test "Logs after the step"
- [ ] 2.28 `doctor` turn-hook self-check for each installed CLI
- [ ] 2.29 Run TLC on `formal/terminals.tla` and its broken variants (`scripts/tla-check.sh` locally, the `tla` job in CI): the model passes, both variants are rejected
- [ ] 2.30 Code review against model.md's mapping table: every terminal start is under the change lock with the change's one name; stop with a free lock takes the lock before `hc kill`

## 3. Activity (agent-activity)

- [ ] 3.1 Current task: first unticked non-ship task of the run's scope (or the change) in the worktree's tasks.md; none → `all tasks in scope ticked`
- [ ] 3.2 Test "Builder in group 2"
- [ ] 3.3 Test "Everything in scope ticked"
- [ ] 3.4 Run process polls tasks.md every 10 s during a step and appends `task_ticked` / `task_unticked` events; document them in `internal/history`
- [ ] 3.5 Test "Two ticks" (fake clock)
- [ ] 3.6 Step and run start times; last activity from the log's last write (headless) or herdr-core's terminal record (terminal)
- [ ] 3.7 Test "Quiet agent"
- [ ] 3.8 Agent state `working` / `waiting for you` from turn ends and herdr-core status; waiting cards count as needing the person
- [ ] 3.9 Test "Permission prompt" (fake `hc` reporting waiting; board and drawer counts)
- [ ] 3.10 `board --json`: `activity` on running, queued and stopped cards; update the board JSON snapshot
- [ ] 3.11 Test "Card with activity"
- [ ] 3.12 Board mod: step, agent and model, `on <n>`, time since last tick, running time and `waiting for you` on cards; every field and a tick timeline in card detail
- [ ] 3.13 Node test "Owner opens a running card"
- [ ] 3.14 Board mod: Watch (`agent.focus {terminal}` on the card's machine) on terminal cards; Follow log kept for headless cards
- [ ] 3.15 Node test "Watch a builder"

## 4. Answers (blocked-answers)

- [ ] 4.1 Question of a blocked run (summary and findings of its `blocked` result, step, round) in `status`, `board --json` and card detail
- [ ] 4.2 Test "Builder blocked on a decision"
- [ ] 4.3 `tuckerbox answer <change> <text|--file|->` under the change lock: append to `answers` in `run.json`, `answered` history event, 16 KiB limit, refuse unless blocked or a terminal agent waits; document it in README
- [ ] 4.4 Test "Answer from the command line"
- [ ] 4.5 Test "Run not blocked"
- [ ] 4.6 Brief section "Answers from a person" with the question and answers, kept in the resumed step's briefs until it reports a result, then marked used
- [ ] 4.7 Test "Resume with the decision" (brief snapshot)
- [ ] 4.8 Test "Two answers"
- [ ] 4.9 `--resume`: record, then resume with all its gates; keep the answer when the resume is refused
- [ ] 4.10 Test "Resume refused after the answer"
- [ ] 4.11 Answer to a waiting terminal agent with `hc send`, recorded in history
- [ ] 4.12 Test "Answer without opening the tab" (fake `hc` records the send)
- [ ] 4.13 Board mod: question and text box on blocked and waiting cards; Answer and Answer and resume run `tuckerbox answer <change> [--resume] -` with the text on standard input
- [ ] 4.14 Node test "Answer and resume from the card"

## 5. Integration

- [ ] 5.1 End to end on a machine with herdr-core: a change's build runs in a terminal tab, the card shows its task moving as it ticks, Watch focuses it, the person answers a question in the tab, the result is accepted, and `stop`/`resume` after killing the run process adopt the live agent
- [ ] 5.2 End to end headless: the same change runs as before with activity on the card, blocks with a question, is answered with `tuckerbox answer --resume`, and finishes
- [ ] 5.3 `go vet ./...`, `go test -race ./...`, `node --test herdr/mods/tuckerbox-board/test/*.test.js`, `CGO_ENABLED=0 go build ./...` and `scripts/tla-check.sh` pass on Linux, macOS and Windows CI

## Workflow follow-up

- Fold `agent-activity` and the board parts of `agent-terminals` and `blocked-answers` into `board-data`, `herdr-board` and `run-history` once `add-herdr-board` is archived.
- Archive this change after the implementation is accepted.
