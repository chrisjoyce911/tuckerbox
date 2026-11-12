---------------------------- MODULE priority_broken_join ----------------------------
\* Admission order against reprioritising and new arrivals (openspec change
\* add-priority). Queued runs wait for a parallel slot; the first in
\* admission order takes the next free one. Three things take the
\* repository's sidecar lock:
\*   - a `tuckerbox priority` move, for all its file writes
\*   - a run joining the queue (publishing its ticket)
\*   - a run's admission check: list the queue, find the first run, and if
\*     it is this run, try for a slot; two steps, both under the lock
\* A run ending frees its slot without the lock.
\* Runs are numbered in request order; ranks are effective priorities.
EXTENDS Naturals, FiniteSets

CONSTANTS NRuns, MaxParallel, Ranks

Runs == 1..NRuns
NoRun == 0

VARIABLES
  st,         \* per run: "new", "waiting", "decided", "running", "done"
  slots,      \* runs holding a parallel slot
  prio,       \* ranks as the files say now
  committed,  \* ranks after the last completed move
  target,     \* ranks the move in progress is writing
  lock,       \* sidecar lock: "free", "mover" or "run"
  holder,     \* the run holding the lock, when lock = "run"
  badOrder    \* a run took a slot while another queued run came first

vars == <<st, slots, prio, committed, target, lock, holder, badOrder>>

Init ==
  /\ st = [r \in Runs |-> "new"]
  /\ slots = {}
  /\ prio \in [Runs -> Ranks]
  /\ committed = prio
  /\ target = prio
  /\ lock = "free"
  /\ holder = NoRun
  /\ badOrder = FALSE

Queued == {r \in Runs : st[r] \in {"waiting", "decided"}}

\* Admission order: lower rank first; equal ranks in request order.
Before(a, b, p) == p[a] < p[b] \/ (p[a] = p[b] /\ a < b)

First(r, p) == \A q \in Queued \ {r} : Before(r, q, p)

\* ---- Runs ----

\* BROKEN: publishing a ticket does not take the sidecar lock, so it can
\* land inside another run's admission check.
Join(r) ==
  /\ st[r] = "new"
  /\ st' = [st EXCEPT ![r] = "waiting"]
  /\ UNCHANGED <<slots, prio, committed, target, lock, holder, badOrder>>

\* Admission, step 1: take the lock, list the queue; this run is first.
Check(r) ==
  /\ st[r] = "waiting"
  /\ lock = "free"
  /\ First(r, prio)
  /\ st' = [st EXCEPT ![r] = "decided"]
  /\ lock' = "run"
  /\ holder' = r
  /\ UNCHANGED <<slots, prio, committed, target, badOrder>>

\* Admission, step 2, still under the lock: try for a slot, then let go.
\* A slot may have been freed in between, by a run ending.
TakeSlot(r) ==
  /\ st[r] = "decided"
  /\ lock = "run"
  /\ holder = r
  /\ IF Cardinality(slots) < MaxParallel
       THEN /\ st' = [st EXCEPT ![r] = "running"]
            /\ slots' = slots \cup {r}
            /\ badOrder' = (badOrder \/ ~First(r, committed))
       ELSE /\ st' = [st EXCEPT ![r] = "waiting"]
            /\ UNCHANGED <<slots, badOrder>>
  /\ lock' = "free"
  /\ holder' = NoRun
  /\ UNCHANGED <<prio, committed, target>>

\* A run ends and frees its slot, without the lock.
Finish(r) ==
  /\ st[r] = "running"
  /\ st' = [st EXCEPT ![r] = "done"]
  /\ slots' = slots \ {r}
  /\ UNCHANGED <<prio, committed, target, lock, holder, badOrder>>

\* ---- tuckerbox priority: a move, one file at a time, under the lock ----

BeginMove ==
  /\ lock = "free"
  /\ \E t \in [Runs -> Ranks] : t # prio /\ target' = t
  /\ lock' = "mover"
  /\ UNCHANGED <<st, slots, prio, committed, holder, badOrder>>

WriteOne(r) ==
  /\ lock = "mover"
  /\ prio[r] # target[r]
  /\ prio' = [prio EXCEPT ![r] = target[r]]
  /\ UNCHANGED <<st, slots, committed, target, lock, holder, badOrder>>

EndMove ==
  /\ lock = "mover"
  /\ prio = target
  /\ committed' = prio
  /\ lock' = "free"
  /\ UNCHANGED <<st, slots, prio, target, holder, badOrder>>

\* Everything ran: stop, so the bound is not reported as a deadlock.
Finished ==
  /\ \A r \in Runs : st[r] = "done"
  /\ lock = "free"
  /\ UNCHANGED vars

Next ==
  \/ \E r \in Runs : Join(r) \/ Check(r) \/ TakeSlot(r) \/ Finish(r) \/ WriteOne(r)
  \/ BeginMove \/ EndMove \/ Finished

Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ st \in [Runs -> {"new", "waiting", "decided", "running", "done"}]
  /\ slots \subseteq Runs
  /\ prio \in [Runs -> Ranks]
  /\ committed \in [Runs -> Ranks]
  /\ lock \in {"free", "mover", "run"}
  /\ holder \in Runs \cup {NoRun}

\* change-runs: parallel limit (as in runs.tla).
WithinLimit == Cardinality(slots) <= MaxParallel

\* change-priority: Queue follows the order. A run takes a slot only when
\* it is first among the queued runs, by the ranks of the last completed
\* move, at the moment it takes the slot.
OrderedAdmission == ~badOrder
=============================================================================
