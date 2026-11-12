# Proposal

## Why

The owner opened a card that was in progress and asked: "I can't tell what
task is being worked on... should I be able to click through to the agent
doing the work, or should the card show enough to see what task and agent
are being worked on?"

Both. Today a card shows the step and the run state, nothing about what the
agent is doing or whether it is stuck. And the agent runs as a hidden child
process: its output goes to a log, nobody can answer it when it asks
something, and a permission prompt it can't show just fails the step.
`add-core-runner` planned `add-herdr-terminals` for the second half; this
change absorbs it.

## What Changes

- **Live activity on cards and in card detail:**
  - the current step, agent, model, effort and round
  - the task the builder is on: the first unticked task of the run's scope
    in the worktree's tasks.md, with the last task ticked and when
  - how long the step and the run have been going, and when the agent last
    did something
  - in a terminal, whether the agent is working or waiting for you
  - Ticks seen during a step become `task_ticked` history events.
- **A terminal backend:**
  - Each step's agent runs in a herdr-core terminal (`hc run --bg`), not as
    a hidden child process.
  - The terminal has one name per change and `herdr.space` and `herdr.tab`
    labels, so the Herdr app shows it as a tab.
  - Built-in agents run in their interactive mode, with the brief as the
    first prompt. The person can watch them and type into them.
- **Watch** on a card focuses the agent's tab on its machine.
- **Results stay as they are.** The agent ends with its result as its
  final message. In a terminal, each CLI's own end-of-turn hook hands that
  message to Tuckerbox (Claude Code's `Stop` hook, Codex's `notify`).
  A turn that ends without a result leaves the step running, with the
  card saying the agent waits for you.
- **Stop and resume stay as they are.**
  - `stop` ends the agent's terminal.
  - If Tuckerbox's run process dies while the agent works, the terminal
    keeps going. `resume` adopts it instead of starting a second agent.
  - TLA+ model `formal/terminals.tla` with two broken variants.
- **Answer a blocked card:**
  - A run blocked with a question shows that question on its card.
  - The person answers on the card, or with `tuckerbox answer <change>
    <text>` (optionally `--resume`).
  - The answer is kept in run state and history, and goes into the brief
    when the run resumes, so the agent continues from the person's
    decision.
  - When a terminal agent waits for you, `answer` types into its terminal.
  - First real case: `fix-board-safety`'s builder blocked on a live Herdr
    check and an install-rollback decision.
- **Headless stays the fallback:** without herdr-core, or with
  `agent_backend = "headless"` in machine config, agents run as today
  (`claude -p`, `codex exec`).

## Capabilities

### New Capabilities

- `agent-activity`: what a run shows about its agent while a step runs:
  the task, ticks, times, waiting state, on the board and in history.
- `blocked-answers`: a blocked run's question, answering it from the
  board or `tuckerbox answer`, and answers in the next brief.
- `agent-terminals`: running agents in herdr-core terminals: choosing the
  backend, names and labels, interactive commands, results through
  end-of-turn hooks, stop, crash and adoption, and Watch.

### Modified Capabilities

- `agent-launch`: `Headless execution and logs` becomes one of two
  backends.
- `repo-config`: `agent_backend` is a machine-only key.

Board and history additions go in `agent-activity`, because `board-data`,
`herdr-board` and `run-history` belong to `add-herdr-board`, which isn't
archived (see design.md).

## Impact

- Depends on `add-task-scope` (a run's scope). Builds on `add-herdr-board`'s
  board, history and mod, whose code is on the base branch; it declares no
  `depends_on` on it, because a run waits until a dependency is ready or
  archived and `add-herdr-board` is neither.
- `internal/agent`: a backend interface with `headless` (today's code) and
  `terminal` (herdr-core) implementations; interactive command templates
  for the built-in agents.
- New command `tuckerbox answer`; run state and history gain answers.
- A hidden `tuckerbox agent-turn` command that the CLIs' end-of-turn hooks
  call to hand over the final message.
- Herdr board mod: activity on cards and in detail, and the Watch action.
- Needs herdr-core (`hc`) on a machine for the terminal backend; without
  it nothing changes.
