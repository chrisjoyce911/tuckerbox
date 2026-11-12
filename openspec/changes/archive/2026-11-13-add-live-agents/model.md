# Model

## Model

With the terminal backend, a step's agent runs in a herdr-core terminal instead of as a child of the `tuckerbox run` process. The agent outlives that process: if it crashes, the agent keeps working. Resume, a person's `stop` and the run process then race over one change with no shared parent process, so run and stop concurrency changes and is modelled.

The activity and answer parts don't need a model: ticks are read by the one run process, and `tuckerbox answer` writes run state under the change lock that `formal/runs.tla` already checks.

### Files

| File | Role |
|---|---|
| `formal/terminals.tla`, `formal/terminals.cfg` | Model; TLC must pass |
| `formal/terminals_broken_roundnames.tla`, `.cfg` | Broken variant: each round's terminal gets its own name, so a live agent is never found and herdr-core never refuses; TLC must reject (`OneAgent`) |
| `formal/liveslots.tla`, `formal/liveslots.cfg` | Model of parallel slots across changes when agents outlive their run process; TLC must pass |
| `formal/liveslots_broken_runlockonly.tla`, `.cfg` | Broken variant: admission looks only at the run lock, as before; TLC must reject (`WithinLimitLive`) |
| `formal/terminals_broken_stopnokill.tla`, `.cfg` | Broken variant: `stop` with no process holding the lock records `stopped` without ending the agent; TLC must reject (`StoppedMeansNoAgent`) |

### Actors and shared state

- **Run processes (`Procs`):** `tuckerbox run`, `resume`, autopilot's crash resume. Each takes the change's run lock (`Begin`), then adopts a live agent terminal or starts a new round's (`Launch`), and accepts the result (`Finish`). Any may die (`Crash`); the OS frees its lock.
- **Agents (`AgentEnds`):** a terminal's agent finishes its turn with a result; Tuckerbox ends its program.
- **A person (`Stop`):** with a run process holding the lock, that process ends the agent and records `stopped`; with the lock free, `stop` takes it, ends the agent by name and records `stopped`.
- **Shared state:** `lock`, `term` (rounds with a live agent terminal, in herdr-core), `ended`, `state`, `round`.
- **Atomic:** the OS lock; `hc run --name`, which herdr-core refuses while a terminal of that name is live; `hc kill <name>`.
- **Not atomic:** a run process's lifetime and its agent's: the agent can outlive it.

### Invariants

| Invariant | Meaning | Named by requirement |
|---|---|---|
| `OneAgent` | A change never has two live agents | agent-terminals: One agent per change |
| `StoppedMeansNoAgent` | A run recorded `stopped`, with no process holding its lock, has no live agent | agent-terminals: Stopping a terminal agent |
| `WithinLimitLive` | Changes with a live run process or a live agent never outnumber the slots | agent-terminals: Orphan agents keep their slot; Adoption takes the agent's own slot |
| `OrphanHoldsSlot` | Every live agent, orphaned or not, holds a slot's agent lock | agent-terminals: Orphan agents keep their slot |
| `TypeOK` | Variables keep their types | (type check) |

### Slots across changes (`liveslots`)

- **Actors:** run processes of several changes (`Admit`, `Launch`, `RunEnds`, `Crash`, `Adopt`) and their agents (`AgentEnds`).
- **Shared state:** each slot's run lock and agent lock, the changes with a live run process, the changes with a live agent.
- **Atomic:** each OS lock. **Not atomic:** a run process's life and its agent's.
- Bounds: `Changes = {a, b, c}`, `Slots = {s1, s2}`, `MaxCrashes = 2`. Three changes for two slots so a queued change is waiting when a crash frees a run lock; two crashes so one change can crash, be adopted and crash again. TLC explores 468 distinct states. The passing model reaches crash, another change admitted to the other slot, and adoption. `liveslots_broken_runlockonly` is rejected within 131 states: a run crashes, and two more changes are admitted while its agent lives.

### Bounds

`Procs = {p1, p2}`, `MaxRounds = 3`.
- Two processes are enough for the original run and a resume after its crash.
- Three rounds allow a start, a crash with adoption or a new round, and a stop and resume.
- `Finished` lets TLC stop once the run is done or the round bound is used up.

TLC (tla2tools 1.7.4, `scripts/tla-check.sh`) explores 122 distinct states in under a second. The passing model is not vacuous: TLC reaches adoption (`Begin`, `Launch`, `Crash`, `Begin`, `Launch` adopting the live agent).
- `terminals_broken_roundnames` is rejected after 6 states: a run starts round 1, its process crashes, a resume starts round 2 beside the live round 1 agent.
- `terminals_broken_stopnokill` is rejected after 5 states: a run starts, its process crashes, `stop` records `stopped` while the agent is still live.
