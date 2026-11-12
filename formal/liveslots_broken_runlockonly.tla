---------------------------- MODULE liveslots_broken_runlockonly ----------------------------
\* Parallel slots when agents outlive their run process (openspec change
\* add-live-agents). Each slot has two OS locks: the run lock, held by the
\* run process from admission until the run ends, and the agent lock, held by
\* `tuckerbox agent-hold`, the small process that starts the agent inside its
\* terminal and lives exactly as long as the agent. A slot is free only when
\* both are free. A run process may crash; the OS frees its lock, but the
\* agent and its agent lock live on. Resume adopts the agent by taking the run
\* lock of the slot whose agent lock it holds.
EXTENDS Naturals, FiniteSets

CONSTANTS Changes, Slots, MaxCrashes

None == "none"

VARIABLES
  runLock,    \* slot -> change whose run process holds its run lock, or None
  agentLock,  \* slot -> change whose agent holds its agent lock, or None
  proc,       \* changes with a live run process (holding a run lock)
  agent,      \* changes with a live agent
  done,       \* changes whose run has ended
  crashes     \* crashes so far (bounded for TLC)
vars == <<runLock, agentLock, proc, agent, done, crashes>>

TypeOK ==
  /\ runLock \in [Slots -> Changes \cup {None}]
  /\ agentLock \in [Slots -> Changes \cup {None}]
  /\ proc \subseteq Changes
  /\ agent \subseteq Changes
  /\ done \subseteq Changes
  /\ crashes \in 0..MaxCrashes

Init ==
  /\ runLock = [s \in Slots |-> None]
  /\ agentLock = [s \in Slots |-> None]
  /\ proc = {}
  /\ agent = {}
  /\ done = {}
  /\ crashes = 0

\* Admission: a queued change takes a slot whose run lock and agent lock are
\* both free (try the run lock, then the agent lock; release and move on if
\* the agent lock is held).
Admit(c) ==
  /\ c \notin proc \cup agent \cup done
  /\ \E s \in Slots :
       /\ runLock[s] = None
       \* BROKEN: admission looks only at the run lock, as before this fix
       /\ runLock' = [runLock EXCEPT ![s] = c]
  /\ proc' = proc \cup {c}
  /\ UNCHANGED <<agentLock, agent, done, crashes>>

\* The run process starts the agent through agent-hold, which takes the agent
\* lock of the run's own slot before it starts the agent.
Launch(c) ==
  /\ c \in proc
  /\ c \notin agent
  /\ \E s \in Slots :
       /\ runLock[s] = c
       /\ agentLock[s] = None
       /\ agentLock' = [agentLock EXCEPT ![s] = c]
  /\ agent' = agent \cup {c}
  /\ UNCHANGED <<runLock, proc, done, crashes>>

\* The agent finishes (or stop ends it); agent-hold exits and the OS frees
\* the agent lock.
AgentEnds(c) ==
  /\ c \in agent
  /\ agent' = agent \ {c}
  /\ agentLock' = [s \in Slots |-> IF agentLock[s] = c THEN None ELSE agentLock[s]]
  /\ UNCHANGED <<runLock, proc, done, crashes>>

\* The run ends once its agent has ended; the run process frees its lock.
RunEnds(c) ==
  /\ c \in proc
  /\ c \notin agent
  /\ proc' = proc \ {c}
  /\ done' = done \cup {c}
  /\ runLock' = [s \in Slots |-> IF runLock[s] = c THEN None ELSE runLock[s]]
  /\ UNCHANGED <<agentLock, agent, crashes>>

\* The run process dies. The OS frees its run lock; the agent lives on.
Crash(c) ==
  /\ c \in proc
  /\ crashes < MaxCrashes
  /\ proc' = proc \ {c}
  /\ runLock' = [s \in Slots |-> IF runLock[s] = c THEN None ELSE runLock[s]]
  /\ crashes' = crashes + 1
  /\ UNCHANGED <<agentLock, agent, done>>

\* Resume adopts the live agent: it takes the run lock of the slot whose agent
\* lock the change's agent holds. It needs no free slot.
Adopt(c) ==
  /\ c \notin proc
  /\ c \in agent
  /\ \E s \in Slots :
       /\ agentLock[s] = c
       /\ runLock[s] = None
       /\ runLock' = [runLock EXCEPT ![s] = c]
  /\ proc' = proc \cup {c}
  /\ UNCHANGED <<agentLock, agent, done, crashes>>

Finished ==
  /\ done = Changes
  /\ UNCHANGED vars

Next ==
  \/ \E c \in Changes :
       Admit(c) \/ Launch(c) \/ AgentEnds(c) \/ RunEnds(c) \/ Crash(c) \/ Adopt(c)
  \/ Finished

Spec == Init /\ [][Next]_vars

\* Changes at work: a live run process or a live agent, orphaned or not.
Busy == proc \cup agent

\* Never more changes at work than there are slots (max_parallel).
WithinLimitLive == Cardinality(Busy) <= Cardinality(Slots)

\* An orphaned agent still holds a slot's agent lock, so its capacity is
\* reserved until it ends or is adopted.
OrphanHoldsSlot == \A c \in agent : \E s \in Slots : agentLock[s] = c
=============================================================================
