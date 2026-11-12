# agent-terminals Specification

## Purpose
Defines how Tuckerbox runs a step's agent in a herdr-core terminal that a person can watch and type into, how it gets the agent's result from there, and how stop, crashes and resume behave when the agent outlives Tuckerbox's own process.

## Requirements

### Requirement: Choosing the backend
Machine config `agent_backend` SHALL be `auto` (default), `terminal` or `headless`. `auto` SHALL mean `terminal` when `hc` is on the PATH and its server answers, else `headless`. With `terminal` and no working `hc`, a run SHALL refuse to start and say why. `tuckerbox doctor` SHALL report the backend in use.

#### Scenario: No herdr-core
- **WHEN** `agent_backend` is not set and `hc` is not installed
- **THEN** agents run headless as before, and `doctor` says the backend is `headless` because herdr-core wasn't found

#### Scenario: Terminal asked for without herdr-core
- **WHEN** machine config sets `agent_backend = "terminal"` and `hc server status` fails
- **THEN** `tuckerbox run` refuses, says herdr-core isn't running, and creates no run state

### Requirement: Agents that run in a terminal
The built-in `claude` and `codex` agents SHALL have an interactive command for the terminal backend. A custom agent MAY define one as `interactive`, with a `{turnhook}` placeholder. An agent without one SHALL run headless, and its card SHALL say so.

#### Scenario: Custom agent without an interactive command
- **WHEN** the terminal backend is in use and a step uses custom agent `omp` with only `command`
- **THEN** that step runs headless and its card's backend is `headless`

### Requirement: Terminal name and labels
Tuckerbox SHALL start a step's agent with `hc run --bg` in the change's worktree, named `tb-` and the first 12 hex digits of a SHA-256 of the repository's remote (or path) and the change id, so every step of a change uses one name. It SHALL label it `herdr.space` (the repository), `herdr.tab` (change and step) and the change, step, round and run id.

#### Scenario: Agent tab in Herdr
- **WHEN** `add-login`'s build step starts in the terminal backend in repository `shop`
- **THEN** herdr-core has a live terminal with that name, in the worktree, labelled for space `shop` and tab `add-login · build`, and the Herdr app shows it as a tab there

### Requirement: One agent per change
Tuckerbox SHALL start a terminal only while holding the change's run lock. A live terminal with the change's name SHALL be adopted when its labels match the run's current step and round, and ended otherwise, before a new one starts. herdr-core's refusal of a second live terminal of that name SHALL stop any start. Model: `formal/terminals.tla`, `OneAgent`.

#### Scenario: Leftover terminal from an older round
- **WHEN** `rerun add-login build` starts round 4 while a terminal labelled round 3 is still live
- **THEN** Tuckerbox ends the round 3 terminal first, and then starts round 4's

### Requirement: Interactive agent with the brief
In a terminal, a built-in agent SHALL run in its interactive mode with the step's prompt (read the brief) as its first message, the same model, effort and profile flags as headless, and the same `TUCKERBOX_*` environment. The person SHALL be able to type to it.

#### Scenario: Person answers a question
- **WHEN** the build agent in its tab asks which of two designs to follow and the person types an answer there
- **THEN** the agent carries on with that answer in the same session

### Requirement: Results from the end of a turn
In a terminal, Tuckerbox SHALL hand the CLI a turn-end hook (Claude Code's `Stop` hook through `--settings`, Codex's `notify` through `-c`) that runs `tuckerbox agent-turn` to write the turn's final message to the step's last-message file. That message SHALL be read with the result contract as today.

#### Scenario: Turn ends with a result
- **WHEN** the build agent's turn ends with a valid result for its change, step and round
- **THEN** Tuckerbox accepts it as today, ends the agent's program after the result grace, and keeps the terminal's history readable until the change's next step starts

#### Scenario: Stale result in a later turn
- **WHEN** a turn ends with a result naming round 2 while the step is on round 3
- **THEN** Tuckerbox ignores it, as for a result file, and keeps waiting

### Requirement: A turn without a result
A turn that ends without a valid result for the step SHALL leave the step running, with its agent state `waiting for you`, and Tuckerbox SHALL read the next turn's final message the same way. It SHALL NOT end the agent for this.

#### Scenario: Agent asks before finishing
- **WHEN** the review agent ends a turn asking whether to also check the README, with no result
- **THEN** the step stays running, the card says `waiting for you`, and the review agent gets the person's answer in its tab

### Requirement: Stopping a terminal agent
`tuckerbox stop` SHALL end the agent's terminal by name and record the run `stopped`. When the run's own process holds the lock it SHALL do this; when no process holds it, `stop` SHALL take the lock and do it. A stopped run SHALL have no live agent. Model: `formal/terminals.tla`, `StoppedMeansNoAgent`.

#### Scenario: Stop after a crash
- **WHEN** the run process of `add-login` died while its build agent kept working in a terminal, and the user runs `tuckerbox stop add-login`
- **THEN** the agent's terminal is ended, the run is `stopped`, and no agent of `add-login` is live

### Requirement: Adopting a live agent
When a run's process died while its agent's terminal lives, `status` and the board SHALL show the run `running` with `not watched`. `tuckerbox resume`, and autopilot's crash resume, SHALL adopt the live terminal when it is the run's current step and round: no new round and no second agent.

#### Scenario: Resume after the run process died
- **WHEN** the machine's `tuckerbox` process for `add-login` was killed while the build agent kept working in round 2, and the user runs `tuckerbox resume add-login`
- **THEN** no new agent starts, the run is still round 2, and the build agent's result is accepted when its turn ends

### Requirement: Orphan agents keep their slot
Each parallel slot SHALL have a run lock and an agent lock. A terminal agent SHALL start through `tuckerbox agent-hold`, which takes its run's slot's agent lock before starting the agent and lives exactly as long as it. Admission SHALL take a slot only when both locks are free. A dispatched run's fleet slot SHALL work the same way. Model: `formal/liveslots.tla`, `WithinLimitLive`, `OrphanHoldsSlot`.

#### Scenario: Crash, then another change queued
- **WHEN** `max_parallel = 1`, `add-login`'s run process dies while its build agent keeps working, and `add-search` is queued
- **THEN** `add-search` stays queued while that agent lives, and starts once the agent ends or is stopped

#### Scenario: Crash with room for another change
- **WHEN** `max_parallel = 2`, `add-login`'s run process dies while its agent keeps working, and `add-search` is queued
- **THEN** `add-search` takes the other slot, and the two agents are the only ones running

#### Scenario: Agent ends while orphaned
- **WHEN** an orphaned agent's turn ends and its program exits
- **THEN** its agent lock is free, and a queued change can take the slot

### Requirement: Adoption takes the agent's own slot
Adopting a live agent SHALL take the run lock of the slot whose agent lock that agent holds, recorded in `run.json`, and SHALL need no free slot. It SHALL wait while an admission briefly holds that run lock to test the agent lock.

#### Scenario: Crash, another change admitted, then adoption
- **WHEN** `max_parallel = 2`, `add-login`'s run process dies, `add-search` is admitted to the other slot, and the user runs `tuckerbox resume add-login`
- **THEN** resume takes `add-login`'s own slot, no third agent starts, and both runs finish within the limit

### Requirement: Logs of a terminal agent
`tuckerbox logs <change> [step] [--follow]` SHALL show the agent terminal's history through herdr-core while it lives. When the step ends, Tuckerbox SHALL append that history to the step's log file, so `logs` still works after the terminal is gone.

#### Scenario: Logs after the step
- **WHEN** a terminal step finished and its terminal was later replaced by the next step's
- **THEN** `tuckerbox logs add-login build` shows the build agent's session from the log file

### Requirement: Watch from the board
A card whose step runs in a terminal SHALL offer Watch, which focuses the agent's terminal on the card's machine with Herdr's `agent.focus {terminal}`, opening its tab if needed. A card running headless SHALL keep Follow log instead.

#### Scenario: Watch a builder
- **WHEN** the owner clicks Watch on `add-login`'s card, whose build agent runs in a terminal on `mini`
- **THEN** Herdr shows that agent's tab with the keyboard in it, and the owner can type to the agent
