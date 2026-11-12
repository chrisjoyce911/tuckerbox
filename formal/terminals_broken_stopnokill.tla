---------------------------- MODULE terminals_broken_stopnokill ----------------------------
\* Agents in herdr-core terminals (openspec change add-live-agents).
\* With the terminal backend a step's agent runs in a herdr-core terminal,
\* not as a child of the `tuckerbox run` process. The terminal outlives that
\* process: if it crashes, the agent keeps going. Resume, a person's `stop`,
\* and the run process race for one change. The atomic primitives:
\*   - the change's run lock (an OS file lock, released when its holder dies)
\*   - `hc run --name <n>`: herdr-core refuses a second live terminal with the
\*     same name; every step of a change uses one name
\*   - `hc kill <name>`: ends the program in that terminal
EXTENDS Naturals, FiniteSets

CONSTANTS Procs, MaxRounds

None == "none"

VARIABLES
  lock,    \* the process holding the change's run lock, or None
  pc,      \* each process's stage
  term,    \* rounds whose agent terminal is live (herdr-core state)
  ended,   \* rounds whose agent has ended with its result
  state,   \* the run's recorded state
  round    \* the run's current round
vars == <<lock, pc, term, ended, state, round>>

Init ==
  /\ lock = None
  /\ pc = [p \in Procs |-> "idle"]
  /\ term = {}
  /\ ended = {}
  /\ state = "idle"
  /\ round = 0

\* `tuckerbox run`, `resume` or autopilot's crash resume takes the run lock.
Begin(p) ==
  /\ pc[p] = "idle"
  /\ lock = None
  /\ state \in {"idle", "running", "stopped"}
  /\ lock' = p
  /\ pc' = [pc EXCEPT ![p] = "locked"]
  /\ UNCHANGED <<term, ended, state, round>>

\* Under the lock: a live terminal with the change's name is adopted (its
\* agent is still at work, e.g. after this machine's run process crashed);
\* otherwise a new round's agent starts with `hc run --name`, which herdr-core
\* refuses while a terminal of that name is live.
Launch(p) ==
  /\ pc[p] = "locked"
  /\ IF term # {}
       THEN UNCHANGED <<term, round>>
       ELSE /\ round < MaxRounds
            /\ round' = round + 1
            /\ term' = {round + 1}
  /\ state' = "running"
  /\ pc' = [pc EXCEPT ![p] = "watch"]
  /\ UNCHANGED <<lock, ended>>

\* An agent finishes its turn with its result; Tuckerbox then ends its program.
AgentEnds(r) ==
  /\ r \in term
  /\ term' = term \ {r}
  /\ ended' = ended \cup {r}
  /\ UNCHANGED <<lock, pc, state, round>>

\* The watching process accepts the current round's result.
Finish(p) ==
  /\ pc[p] = "watch"
  /\ round \in ended
  /\ state' = "done"
  /\ lock' = None
  /\ pc' = [pc EXCEPT ![p] = "idle"]
  /\ UNCHANGED <<term, ended, round>>

\* The run process dies (or the machine sleeps it away): the OS frees its lock.
\* The agent's terminal lives on in herdr-core.
Crash(p) ==
  /\ pc[p] \in {"locked", "watch"}
  /\ lock = p
  /\ lock' = None
  /\ pc' = [pc EXCEPT ![p] = "idle"]
  /\ UNCHANGED <<term, ended, state, round>>

\* `tuckerbox stop`. With a live run process holding the lock, that process
\* ends the agent and records `stopped`. With the lock free (no process),
\* stop takes the lock itself, ends the agent by name, records `stopped` and
\* lets go: one step under the lock.
StopKills == term \* BROKEN: stop records stopped without ending an agent no process watches

Stop ==
  /\ state = "running"
  /\ \/ /\ lock # None
        /\ pc[lock] = "watch"
        /\ term' = {}
        /\ pc' = [pc EXCEPT ![lock] = "idle"]
     \/ /\ lock = None
        /\ term' = StopKills
        /\ UNCHANGED pc
  /\ state' = "stopped"
  /\ lock' = None
  /\ UNCHANGED <<ended, round>>

Finished ==
  /\ \/ state = "done"
     \/ round = MaxRounds
  /\ UNCHANGED vars

Next ==
  \/ \E p \in Procs : Begin(p) \/ Launch(p) \/ Finish(p) \/ Crash(p)
  \/ \E r \in 1..MaxRounds : AgentEnds(r)
  \/ Stop
  \/ Finished

Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ lock \in Procs \cup {None}
  /\ pc \in [Procs -> {"idle", "locked", "watch"}]
  /\ term \subseteq 1..MaxRounds
  /\ ended \subseteq 1..MaxRounds
  /\ state \in {"idle", "running", "stopped", "done"}
  /\ round \in 0..MaxRounds

\* agent-terminals: a change never has two live agents.
OneAgent == Cardinality(term) <= 1

\* agent-terminals: a change recorded as stopped, with no process holding its
\* lock, has no live agent.
StoppedMeansNoAgent == (state = "stopped" /\ lock = None) => term = {}
=============================================================================
