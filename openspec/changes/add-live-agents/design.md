# Design

## Context

See proposal.md for why. What exists:
- `internal/agent.Run` starts the agent with `exec.Command` in its own process group, copies stdout and stderr to the step's log, polls for a result file, and reads the final message from stdout (Claude's stream-json `result` event) or from the file Codex writes with `-o`. `stop` sends SIGTERM to the group, then SIGKILL after a grace period.
- The run process (`tuckerbox run` in the background) holds the change's run lock while queued or running (`formal/runs.tla`). `stop` reaches the agent through it.
- `add-herdr-board` (on the base branch, not archived): `board --json`, the board mod with Follow log, run history.
- `add-task-scope` (approved): a run's scope, and when a scoped run is ready.
- herdr-core (`hc`): terminals that live in its server, not in the process that started them. `hc run --bg --name <n> --label k=v --cwd <dir>` starts one; a name is unique among live terminals; `hc kill`, `hc read`, `hc ls --json` (with each agent terminal's `status`: working or waiting). Terminals labelled `herdr.space` and `herdr.tab` show as tabs in the Herdr app. The Herdr mod API offers `agent.focus {terminal}`.

## Goals / Non-Goals

**Goals:**
- From a card, see which task and which agent are at work, and whether it is stuck or waiting.
- From a card, go to the agent and talk to it.
- Results, stop, resume and the one-run-per-change rule keep working, also when the agent outlives Tuckerbox's process.

**Non-Goals:**
- **Parsing an agent's tool calls or its own todo list.** See "Where the task comes from".
- **Several agents on one change at once.** That is `add-parallel-steps`.
- **Keeping an agent's session across steps** (the builder's conversation continuing into round 2). Each step still starts a fresh session from its brief.
- **Terminal agents on another machine than the run.** The terminal runs on the run's machine; Watch reaches it through Herdr's machine connection.
- **A daemon.** herdr-core already runs as a server for anyone who uses it; Tuckerbox adds none.

## Decisions

### Where the task comes from

Three sources were considered for "the task the builder is on":

| Source | For | Against |
|---|---|---|
| **Ticks in the worktree's tasks.md** | Agent-agnostic: works for Claude, Codex, custom agents, both backends. Already the truth for scope completion (`add-task-scope`). Needs nothing new from the agent. | Lags: a task shows as current until it is ticked, so "on 2.3" means "2.2 done, 2.3 next". |
| Stream-json events (Claude's `TodoWrite`, tool calls) | Fine-grained and immediate. | Claude-only and headless-only: Codex's `--json` events differ, custom agents have none, and an interactive terminal has no stream-json at all. A todo list is the agent's private plan; its items don't map to task numbers. |
| An explicit progress line in the result contract | Precise if agents comply. | The result is the *final* message; progress would need a second, mid-run channel, such as a file the agent writes. A read-only reviewer can't write one, and every custom agent would have to learn it. |

Chosen: **ticks**. The first unticked task of the scope is the honest answer to "what is it on": everything before it is done by the agent's own account, which is the same account the scope rule trusts. The lag is shown, not hidden: the card says what was last ticked and when ("2.2 ticked 4 min ago"), and last activity says whether the agent is busy. For a review step the same rule gives "all tasks in scope ticked", which is what a reviewer sees.

The run process polls tasks.md every 10 seconds while a step runs; it already polls for the result file. Each change of a box becomes a `task_ticked` or `task_unticked` history event, so the card detail can show a timeline and the time since the last tick, with no new state file. The board reads the current task from the worktree, as it already does for progress.

### Last activity and waiting

- **Headless:** the log file's last write, which is the agent's last output.
- **Terminal:** herdr-core's terminal record (`hc ls --json` / `hc get --json`), which has the agent's status (working, or waiting for input) and its last output. `waiting for you` also comes from Tuckerbox's own knowledge: a turn ended without a result.

Waiting cards count as needing the person, in the board header and the drawer, like blocked runs.

### The terminal backend

```
agent_backend = "auto"      # machine config: "auto" (default), "terminal" or "headless"
```

`internal/agent` gets a backend interface with two implementations. `headless` is today's `Run`, unchanged. `terminal`:

1. Under the change lock, look for a live terminal named `tb-<hash>` (`hc get --json`). Adopt it if its `tuckerbox.step` and `tuckerbox.round` labels match; otherwise `hc kill` it and wait for it to end.
2. Write the brief as today.
3. `hc run --bg --name tb-<hash> --cwd <worktree> --label herdr.space=<repo> --label herdr.tab="<change> · <step>" --label tuckerbox.change=… --label tuckerbox.step=… --label tuckerbox.round=… --label tuckerbox.run=… --no-parent --outlive-parent -- <interactive argv>`.
4. Record the terminal's address in `run.json`.
5. Poll: the last-message file (written by the turn hook), the result file, tasks.md, and the terminal's status, as `Run` polls today.
6. On a valid result: after the result grace, `hc kill` the terminal and append `hc read` output to the step's log. The finished terminal is left for herdr-core to show; the next step's start replaces it (same name, once the old one has ended).

The name is a hash, because herdr-core names are at most 32 characters of `a-z0-9-_`. The hash covers the repository's normalised remote (or its path without one) and the change id, so two repositories' `add-login` don't collide. One name per change, not per step or round, is what makes herdr-core's name check a lock on "one live agent per change" (next section).

`--no-parent --outlive-parent` because the run process is not the agent's owner in herdr-core's sense: the agent must survive it.

Why not keep the headless command and only show it in a terminal: `claude -p` and `codex exec` are not interactive. The person could watch, but not answer a question or a permission prompt. Interactive mode is the point.

### Interactive commands and the turn hook

| Agent | Interactive argv | Turn hook |
|---|---|---|
| `claude` | `claude --model {model} --effort {effort} {permissions} --settings <json> "<prompt>"` | `--settings '{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"tuckerbox agent-turn --claude …"}]}]}}'`: Claude Code runs it at the end of every turn with JSON on stdin naming the session's transcript; `agent-turn` reads the last assistant message from it. |
| `codex` | `codex -C {workdir} -m {model} -c model_reasoning_effort={effort} {permissions} -c notify=[…] "<prompt>"` | `notify`: Codex runs it after each turn with JSON whose `last-assistant-message` is the final message. |

`tuckerbox agent-turn --state <dir> --step <s> --round <r>` is hidden. It writes the message atomically to the step's last-message file, and nothing else. Both hooks are passed on the command line, so no user or project config file is changed. The hooks run as the person's own commands, outside any sandbox the agent applies to the model's tool calls, which is what lets a read-only reviewer report, as `-o` does today.

The result rules are untouched: the message must hold a well-formed result for this change, step and round; a result file still wins if it differs. The only new case is a turn that ends without one. Headless, that means the process exited, so the step fails. In a terminal it means the agent is waiting for the person, so the step stays running.

A custom agent can opt in with `interactive = [...]` and a `{turnhook}` placeholder, expanded to the `tuckerbox agent-turn …` argv. Without it, the step runs headless, and the card says so.

### Stop, crashes and adoption

The agent no longer dies with the run process. That changes run and stop concurrency, so it is modelled (`formal/terminals.tla`, model.md).
- **One live agent per change.** Tuckerbox starts a terminal only while holding the change lock, and every start uses the change's one terminal name, which herdr-core refuses while a terminal of that name is live. A run process that finds a live one adopts it or ends it first; it never starts beside it. `terminals_broken_roundnames` gives each round its own name, and TLC finds the second live agent after a crash and resume.
- **Stop.** With a live run process holding the lock, `stop` signals it as today, and that process ends the terminal and records `stopped`. With no process holding the lock (the run process died), `stop` takes the lock itself, ends the terminal by name, records `stopped`, and lets go. `terminals_broken_stopnokill` records `stopped` without ending the unwatched agent; TLC finds the stopped run with a live agent.
- **Crash.** The OS frees the lock; the agent keeps working. `status` sees the run `running` with no live process (as today's crash detection does) and a live terminal, and shows `not watched`. `resume`, or autopilot's crash resume, adopts the terminal: no new round, no second agent, and the agent's result is accepted when its turn ends. With no live terminal, resume starts a new round as today.

### Orphan agents keep their slot (`formal/liveslots.tla`)

A parallel slot today is an OS lock (`slots/<n>.lock`) held by the run process. With an agent that outlives that process, a crash would free the slot while the agent keeps working, and a queued change could start a second agent beyond `max_parallel`. Adoption later can't undo that.

Each slot gets a second lock, `slots/<n>.agent.lock`:
- **Admission** takes the run lock of a slot, then tries its agent lock. If the agent lock is held, it lets go of the run lock and tries the next slot. A slot is free only when both are free.
- **`tuckerbox agent-hold --slot <n> -- <argv>`** is what the terminal runs. It takes slot `n`'s agent lock, then starts the agent as its child and waits for it, passing its exit status through. It holds no other state and does no other work. When the agent exits, or `stop` kills the terminal, it exits and the OS frees the lock. It refuses to start the agent if the lock is held.
- **Crash:** the run lock is freed; the agent lock is not. The slot stays taken while the agent lives.
- **Adoption** reads the slot number from `run.json` and takes that slot's run lock. It needs no free slot, so it never waits behind queued changes for capacity it already has. An admission that is testing that slot holds its run lock only for a moment; adoption waits for it.
- **Fleet slots** (`add-operating-modes`, machine-wide `fleet_capacity`) get the same pair. A dispatched run passes both slot numbers to `agent-hold`.

Alternatives considered:
- **Adopt before any admission.** Every admission would first look for orphaned agents in herdr-core (`hc ls`) and adopt or count them. But the check and the admission aren't atomic: a run process can die between them. It also makes admission depend on herdr-core being reachable.
- **Hand the run lock to the agent's terminal.** OS locks can't be passed between processes, and releasing then retaking leaves a gap another admission can use.
- **A keeper process per slot outside the terminal.** That is a daemon in all but name. `agent-hold` lives inside the agent's own terminal and ends with it.

Headless agents stay as today, children of the run process, without `agent-hold`. A headless agent that outlives a killed run process is an existing gap, not one this change opens; `agent-hold` could cover it later.

### Answering a blocked run

A step that reports `blocked` usually asks something: "I can't verify this without a live Herdr; should I keep the install rollback?" Today the only way to answer is to edit a file the agent will read, or to rerun and hope. Now:

- **The question** is the blocked result's `summary` and `findings`, already in the result contract and run state. Nothing new is asked of agents; the role prompts already say to block with a clear question.
- **`tuckerbox answer <change> <text>`** (`--file`, or `-` for standard input) appends `{text, by, at, step, round}` to `answers` in `run.json` and writes an `answered` history event, under the change lock. It refuses unless the run is `blocked` (or a terminal agent waits; below). `by` is git `user.name`, as for approvals. 16 KiB per answer keeps briefs bounded.
- **The brief.** On resume, the resumed step's brief gets a section "Answers from a person", with the question and each answer since it, oldest first, saying these are the person's decisions for this step. They stay in that step's briefs (a crash resume, a resume after a stop) until the step reports a result; then they count as used and later briefs leave them out, though history keeps them.
- **`--resume`** answers and resumes in one go, the common case from the board. Resume keeps all its gates (settings approval, team rules); if it refuses, the answer is still recorded and resuming later uses it.
- **A waiting terminal agent** needs no resume: its session is alive. `answer` sends the text into its terminal with `hc send`, which is what typing in the tab does, and records it in history so the card shows what was said.
- **Approval.** An answer is run state, not part of the change's files, so it doesn't touch the approval hash, just as a person typing to a terminal agent doesn't. The review step still reviews what the builder did with it, and history records who decided what.

Board: card detail of a blocked card, or a waiting one, shows the question and a text box with Answer and, on blocked cards, Answer and resume. The mod passes the text on standard input (`tuckerbox answer <change> [--resume] -`) so quoting and newlines are safe.

### Watch

The board mod's Watch calls Herdr's `agent.focus {terminal}` with the address from the card, on the card's machine. Herdr opens the tab from the `herdr.space` and `herdr.tab` labels if it isn't showing. Follow log stays for headless cards.

### Logs

`tuckerbox logs` reads the terminal through `hc read` (and follows it) while it lives, and the step's log file afterwards. At the end of a step the terminal's history is appended to the log file, so the step's record doesn't depend on herdr-core keeping finished terminals.

### How this fits `add-herdr-board`

`board-data`, `herdr-board` and `run-history` are in `add-herdr-board`, which is not archived, so this change writes no deltas against them, as earlier changes didn't:
- `Activity in board data` and `Activity on the board` add to `board-data`'s card and `herdr-board`'s card detail.
- `Ticks in history` adds `task_ticked` and `task_unticked` to `run-history`'s events.
- `Watch from the board` and `Answering from the board` add to `herdr-board`'s actions.
- `answered` adds to `run-history`'s events.
- `agent-launch` is archived, so its `Headless execution and logs` is MODIFIED here.

### Model

| Model | Code (under `internal/`) |
|---|---|
| `Begin`: the run lock | `runs` change lock, as today |
| `Launch`: adopt or start under the lock; `hc run --name` refused while the name is live | `agent/terminal.start` |
| `Stop` with the lock held / free | `runs.Stop` → signal; or take the lock, `hc kill <name>`, write `stopped` |
| `Crash` | the OS releasing the lock; the terminal untouched |

| `liveslots` model | Code (under `internal/`) |
|---|---|
| `Admit`: both slot locks free | `runs.TrySlot`, testing `<n>.agent.lock` |
| `Launch`: agent lock of the run's own slot | `tuckerbox agent-hold --slot <n>` |
| `Crash`: run lock freed, agent lock kept | the OS releasing the run process's locks |
| `Adopt`: run lock of the agent's slot | `resume`, slot number from `run.json` |

`OneAgent`, `StoppedMeansNoAgent`, `WithinLimitLive` and `OrphanHoldsSlot` are named by `agent-terminals` requirements. `scripts/tla-check.sh` runs every model and variant.

## Risks / Trade-offs

- **[CLI hook formats change]** → `agent-turn` reads only the transcript path (Claude) or `last-assistant-message` (Codex). A version that changes them gives no last message, so the step waits with `waiting for you`, and the person sees why in the tab. `doctor` runs a turn-hook self-check with each installed CLI.
- **[The person types something that changes the task]** → It is their call; the result still has to be valid, and the review step still reviews.
- **[An agent waiting forever]** → The card shows `waiting for you` and counts as needing the person. There is no timeout, as there is none for a headless step today.
- **[Ticks lag behind the work]** → Shown honestly: "on 2.3, 2.2 ticked 4 min ago", plus last activity.
- **[A ticking agent that never ticks]** → The card says "on 2.1" with no ticks; last activity still shows it working. Briefs already tell builders to tick as they go.
- **[herdr-core restarts]** → Terminals it parks or loses end the agent. Tuckerbox sees the terminal gone without a result and fails the step as for an agent that exited, and `resume` starts a new round.
- **[Interactive permission prompts]** → In a terminal, a step without a bypass profile can ask before acting. That is a feature (the person answers), but an unattended run then waits. Repositories that want unattended steps keep their machine profiles.

## Migration Plan

- Machines without herdr-core see no change (`auto` → headless).
- Machines with herdr-core switch to terminals on upgrade. `agent_backend = "headless"` in machine config keeps the old behaviour.
- Runs in progress at upgrade keep their headless agent; the next step starts in a terminal.
- Rollback: `agent_backend = "headless"`.

## Open Questions

- The exact JSON each CLI version passes to its turn hook is checked when `agent-turn` is written; `doctor`'s self-check covers drift. It doesn't change the specs.
