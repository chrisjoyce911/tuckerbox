---------------------------- MODULE runs_broken_lock ----------------------------
\* Tuckerbox runs on one machine (openspec change add-core-runner).
\* Several `tuckerbox run` processes compete for changes and parallel slots;
\* agents report through a result file that can hold a stale earlier round.
EXTENDS Naturals, FiniteSets

CONSTANTS Procs, Changes, MaxParallel, MaxRounds

None == "none"

VARIABLES pc, target, lock, slots, round, result, staleAccepted
vars == <<pc, target, lock, slots, round, result, staleAccepted>>

Init ==
  /\ pc = [p \in Procs |-> "idle"]
  /\ target = [p \in Procs |-> None]
  /\ lock = [c \in Changes |-> None]
  /\ slots = {}
  /\ round = [c \in Changes |-> 1]
  /\ result = [c \in Changes |-> 0]
  /\ staleAccepted = FALSE

\* BROKEN: check-then-set lock in two steps (no atomic file lock).
Pick(p, c) ==
  /\ pc[p] = "idle"
  /\ lock[c] = None
  /\ target' = [target EXCEPT ![p] = c]
  /\ pc' = [pc EXCEPT ![p] = "checked"]
  /\ UNCHANGED <<lock, slots, round, result, staleAccepted>>

SetLock(p) ==
  /\ pc[p] = "checked"
  /\ lock' = [lock EXCEPT ![target[p]] = p]
  /\ pc' = [pc EXCEPT ![p] = "locked"]
  /\ UNCHANGED <<target, slots, round, result, staleAccepted>>

\* A queued run takes a parallel slot only while one is free.
TakeSlot(p) ==
  /\ pc[p] = "locked"
  /\ Cardinality(slots) < MaxParallel
  /\ slots' = slots \cup {p}
  /\ pc' = [pc EXCEPT ![p] = "running"]
  /\ UNCHANGED <<target, lock, round, result, staleAccepted>>

\* The current round's agent writes its result.
AgentWrites(p) ==
  /\ pc[p] = "running"
  /\ result' = [result EXCEPT ![target[p]] = round[target[p]]]
  /\ UNCHANGED <<pc, target, lock, slots, round, staleAccepted>>

\* A late agent from an earlier round overwrites the file.
LateWrite(c) ==
  /\ round[c] > 1
  /\ \E r \in 1..(round[c] - 1) : result' = [result EXCEPT ![c] = r]
  /\ UNCHANGED <<pc, target, lock, slots, round, staleAccepted>>

AcceptGuard(c) == result[c] = round[c]

Release(p) ==
  /\ lock' = [lock EXCEPT ![target[p]] = None]
  /\ slots' = slots \ {p}
  /\ pc' = [pc EXCEPT ![p] = "idle"]
  /\ target' = [target EXCEPT ![p] = None]

\* The engine accepts a result: rejection starts the next round,
\* otherwise (done, or the round limit) the run ends and frees its locks.
Accept(p) ==
  LET c == target[p] IN
  /\ pc[p] = "running"
  /\ AcceptGuard(c)
  /\ staleAccepted' = (staleAccepted \/ result[c] # round[c])
  /\ \/ /\ round[c] < MaxRounds
        /\ round' = [round EXCEPT ![c] = round[c] + 1]
        /\ UNCHANGED <<pc, target, lock, slots, result>>
     \/ /\ Release(p)
        /\ UNCHANGED <<round, result>>

Next ==
  \/ \E p \in Procs, c \in Changes : Pick(p, c)
  \/ \E p \in Procs : SetLock(p) \/ TakeSlot(p) \/ AgentWrites(p) \/ Accept(p)
  \/ \E c \in Changes : LateWrite(c)

Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ pc \in [Procs -> {"idle", "checked", "locked", "running"}]
  /\ lock \in [Changes -> Procs \cup {None}]
  /\ slots \subseteq Procs
  /\ round \in [Changes -> 1..MaxRounds]
  /\ result \in [Changes -> 0..MaxRounds]

Active(p) == pc[p] \in {"locked", "running"}

\* change-runs: at most one active run per change.
OneRunPerChange ==
  \A p, q \in Procs : (p # q /\ Active(p) /\ Active(q)) => target[p] # target[q]

\* change-runs: parallel limit.
WithinLimit ==
  Cardinality({p \in Procs : pc[p] = "running"}) <= MaxParallel

\* agent-launch: a stale result is never accepted.
StaleNeverAccepted == ~staleAccepted
=============================================================================
