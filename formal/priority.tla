---------------------------- MODULE priority ----------------------------
\* Admission order against reprioritising (openspec change add-priority).
\* Queued runs wait for a parallel slot; the first in admission order takes
\* the next free one. `tuckerbox priority` moves change the ranks, one file
\* write at a time, while holding the repository's sidecar lock. A run
\* chooses the first run and takes a slot in one step under that lock.
\* Runs are numbered in request order; ranks are effective priorities.
EXTENDS Naturals, FiniteSets

CONSTANTS NRuns, MaxParallel, Ranks

Runs == 1..NRuns
None == 0

VARIABLES
  st,         \* per run: "new", "waiting", "decided", "running", "done"
  slots,      \* runs holding a parallel slot
  prio,       \* ranks as the files say now
  committed,  \* ranks after the last completed move
  target,     \* ranks the move in progress is writing
  lock,       \* sidecar lock: "free", "mover"
  badOrder    \* a run took a slot while another queued run came first

vars == <<st, slots, prio, committed, target, lock, badOrder>>

Init ==
  /\ st = [r \in Runs |-> "new"]
  /\ slots = {}
  /\ prio \in [Runs -> Ranks]
  /\ committed = prio
  /\ target = prio
  /\ lock = "free"
  /\ badOrder = FALSE

Queued == {r \in Runs : st[r] \in {"waiting", "decided"}}

\* Admission order: lower rank first; equal ranks in request order.
Before(a, b, p) == p[a] < p[b] \/ (p[a] = p[b] /\ a < b)

First(r, p) == \A q \in Queued \ {r} : Before(r, q, p)

\* ---- Runs ----

Join(r) ==
  /\ st[r] = "new"
  /\ st' = [st EXCEPT ![r] = "waiting"]
  /\ UNCHANGED <<slots, prio, committed, target, lock, badOrder>>

\* Under the sidecar lock: choose the first run and take a free slot.
Admit(r) ==
  /\ st[r] = "waiting"
  /\ lock = "free"
  /\ Cardinality(slots) < MaxParallel
  /\ First(r, prio)
  /\ st' = [st EXCEPT ![r] = "running"]
  /\ slots' = slots \cup {r}
  /\ badOrder' = (badOrder \/ ~First(r, committed))
  /\ UNCHANGED <<prio, committed, target, lock>>

Finish(r) ==
  /\ st[r] = "running"
  /\ st' = [st EXCEPT ![r] = "done"]
  /\ slots' = slots \ {r}
  /\ UNCHANGED <<prio, committed, target, lock, badOrder>>

\* ---- tuckerbox priority: a move, one file at a time, under the lock ----

BeginMove ==
  /\ lock = "free"
  /\ \E t \in [Runs -> Ranks] : t # prio /\ target' = t
  /\ lock' = "mover"
  /\ UNCHANGED <<st, slots, prio, committed, badOrder>>

WriteOne(r) ==
  /\ lock = "mover"
  /\ prio[r] # target[r]
  /\ prio' = [prio EXCEPT ![r] = target[r]]
  /\ UNCHANGED <<st, slots, committed, target, lock, badOrder>>

EndMove ==
  /\ lock = "mover"
  /\ prio = target
  /\ committed' = prio
  /\ lock' = "free"
  /\ UNCHANGED <<st, slots, prio, target, badOrder>>

\* Everything ran: stop, so the bound is not reported as a deadlock.
Finished ==
  /\ \A r \in Runs : st[r] = "done"
  /\ lock = "free"
  /\ UNCHANGED vars

Next ==
  \/ \E r \in Runs : Join(r) \/ Admit(r) \/ Finish(r) \/ WriteOne(r)
  \/ BeginMove \/ EndMove \/ Finished

Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ st \in [Runs -> {"new", "waiting", "decided", "running", "done"}]
  /\ slots \subseteq Runs
  /\ prio \in [Runs -> Ranks]
  /\ committed \in [Runs -> Ranks]
  /\ lock \in {"free", "mover"}

\* change-runs: parallel limit (as in runs.tla).
WithinLimit == Cardinality(slots) <= MaxParallel

\* change-priority: Queue follows the order. A run takes a slot only when
\* it is first among the queued runs by the ranks of the last completed move.
OrderedAdmission == ~badOrder
=============================================================================
