---------------------------- MODULE scope ----------------------------
\* Dependency readiness with task scopes (openspec change add-task-scope).
\* Change A runs group by group. Each group is built (its tasks ticked) and
\* then reviewed; review done makes the run ready. Change B depends on A and
\* may start only once all of A's work has passed review. A's run writes its
\* state under A's lock; B reads it without that lock.
EXTENDS Naturals

CONSTANTS Groups

VARIABLES
  aState,    \* A's run state in run.json: "active" or "ready"
  ticked,    \* groups ticked in A's tasks.md
  reviewed,  \* groups whose review passed
  complete,  \* run.json: the whole change was ticked when A became ready
  bPc,       \* B: "wait", "sawReady" (broken variant only), "started"
  badStart   \* B started while some of A's work had not passed review

vars == <<aState, ticked, reviewed, complete, bPc, badStart>>

Init ==
  /\ aState = "active"
  /\ ticked = 0
  /\ reviewed = 0
  /\ complete = FALSE
  /\ bPc = "wait"
  /\ badStart = FALSE

BUnchanged == UNCHANGED <<bPc, badStart>>

\* ---- A's run, each step under A's change lock ----

\* The builder ticks the tasks of the group in scope.
Tick ==
  /\ aState = "active"
  /\ ticked = reviewed
  /\ ticked < Groups
  /\ ticked' = ticked + 1
  /\ UNCHANGED <<aState, reviewed, complete>> /\ BUnchanged

\* Review passes: the run is ready, and the same run.json write records
\* whether the whole change is ticked.
ReviewDone ==
  /\ aState = "active"
  /\ ticked = reviewed + 1
  /\ reviewed' = ticked
  /\ aState' = "ready"
  /\ complete' = (ticked = Groups)
  /\ UNCHANGED ticked /\ BUnchanged

\* The next group starts (tuckerbox run --tasks next): the run is active.
NextScope ==
  /\ aState = "ready"
  /\ reviewed < Groups
  /\ aState' = "active"
  /\ complete' = FALSE
  /\ UNCHANGED <<ticked, reviewed>> /\ BUnchanged

\* ---- B's dependency check: one read of A's run.json ----

CheckDep ==
  /\ bPc = "wait"
  /\ aState = "ready"
  /\ complete
  /\ bPc' = "started"
  /\ badStart' = (badStart \/ reviewed < Groups)
  /\ UNCHANGED <<aState, ticked, reviewed, complete>>

Finished ==
  /\ bPc = "started"
  /\ UNCHANGED vars

Next == Tick \/ ReviewDone \/ NextScope \/ CheckDep \/ Finished

Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ aState \in {"active", "ready"}
  /\ ticked \in 0..Groups
  /\ reviewed \in 0..Groups
  /\ complete \in BOOLEAN
  /\ bPc \in {"wait", "sawReady", "started"}

\* task-scope: Dependencies wait for the whole change. B starts only on an
\* observation in which all of A's work had passed review.
DependentSeesReviewedWork == ~badStart
=============================================================================
