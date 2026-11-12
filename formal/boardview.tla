---------------------------- MODULE boardview ----------------------------
\* The Herdr board mod's reads (openspec change fix-board-safety): the board
\* window and the drawer each read `tuckerbox board --json` for the shown
\* space's folders and machine, the "key". Reads are asynchronous: the person
\* may switch spaces while one runs, reads for different keys may overlap, and
\* they may finish in any order. The view must never show a result for a key
\* other than the current one, and must settle on the current key's result.
EXTENDS Naturals

CONSTANTS Keys, First, MaxSwitches

None == "none"

VARIABLES
  want,      \* the key of the shown space now
  shown,     \* the key whose result the view shows (None: nothing yet)
  inflight,  \* the keys with a read running
  switches   \* how many space switches happened (bounded for TLC)
vars == <<want, shown, inflight, switches>>

Init ==
  /\ want = First
  /\ shown = None
  /\ inflight = {}
  /\ switches = 0

\* The shown space changes (A -> B -> A ... is allowed). The view stops showing
\* the old result at once.
Switch(k) ==
  /\ k # want
  /\ switches < MaxSwitches
  /\ want' = k
  /\ shown' = None
  /\ switches' = switches + 1
  /\ UNCHANGED inflight

\* A read starts for the current key: at once after a switch, or on the poll
\* timer. At most one read per key runs at a time; reads for other keys may
\* still be running.
Start ==
  /\ want \notin inflight
  /\ inflight' = inflight \cup {want}
  /\ UNCHANGED <<want, shown, switches>>

\* A read finishes. Its result is applied only if its key is still current.
Apply(k) == IF k = want THEN k ELSE shown

Complete(k) ==
  /\ k \in inflight
  /\ inflight' = inflight \ {k}
  /\ shown' = Apply(k)
  /\ UNCHANGED <<want, switches>>

Next == Start \/ \E k \in Keys : Switch(k) \/ Complete(k)

Spec == Init /\ [][Next]_vars /\ WF_vars(Start) /\ \A k \in Keys : WF_vars(Complete(k))

TypeOK ==
  /\ want \in Keys
  /\ shown \in Keys \cup {None}
  /\ inflight \subseteq Keys
  /\ switches \in 0..MaxSwitches

\* board-view: the view never shows a result for a key that isn't current.
ShownIsCurrent == shown \in {None, want}

\* board-view: once switches stop, the view comes to show the current key's
\* result and keeps it.
Settles == <>[](shown = want)
=============================================================================
