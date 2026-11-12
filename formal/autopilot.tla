---------------------------- MODULE autopilot ----------------------------
\* The autopilot pickup loop (openspec change add-autopilot).
\* Autopilot reads a change's run state and approval, then later starts or
\* resumes the run. A person and the run itself change both in between.
\* The run engine re-checks, under the change lock, that the run is still
\* in the state autopilot saw and the change is still approved.
EXTENDS Naturals

CONSTANTS Changes, Groups, MaxAutoResumes

None == "none"

\* Run states. "stopped" is a stop by a person; "crashed" is a run whose
\* process died. "done" means every group ran and the run went on to ship.
States == {"new", "active", "ready", "blocked", "stopped", "crashed", "done"}

VARIABLES
  state,      \* run state per change, in run.json
  approved,   \* approval state per change is approved (from the files)
  left,       \* task groups not yet ticked
  resumes,    \* autopilot resumes of the current crash, read from history
  pc,         \* autopilot: "idle" or "read"
  target,     \* the change autopilot looked at
  seen,       \* what it saw: [st, ap]
  badStart,   \* autopilot started a change that was not approved or not startable
  badResume   \* autopilot resumed a run that was not crashed

vars == <<state, approved, left, resumes, pc, target, seen, badStart, badResume>>

Init ==
  /\ state = [c \in Changes |-> "new"]
  /\ approved \in [Changes -> BOOLEAN]
  /\ left = [c \in Changes |-> Groups]
  /\ resumes = [c \in Changes |-> 0]
  /\ pc = "idle"
  /\ target = None
  /\ seen = [st |-> "new", ap |-> FALSE]
  /\ badStart = FALSE
  /\ badResume = FALSE

AutoUnchanged == UNCHANGED <<pc, target, seen, badStart, badResume>>

\* ---- The run (engine), one group per run attempt ----

\* A group finishes: the next group waits, or the change is done (ships).
Finish(c) ==
  /\ state[c] = "active"
  /\ left' = [left EXCEPT ![c] = left[c] - 1]
  /\ state' = [state EXCEPT ![c] = IF left[c] = 1 THEN "done" ELSE "ready"]
  /\ UNCHANGED <<approved, resumes>> /\ AutoUnchanged

Block(c) ==
  /\ state[c] = "active"
  /\ state' = [state EXCEPT ![c] = "blocked"]
  /\ UNCHANGED <<approved, left, resumes>> /\ AutoUnchanged

Crash(c) ==
  /\ state[c] = "active"
  /\ state' = [state EXCEPT ![c] = "crashed"]
  /\ UNCHANGED <<approved, left, resumes>> /\ AutoUnchanged

\* A step edits the change's text: the approval goes stale mid-run.
EditText(c) ==
  /\ state[c] = "active"
  /\ approved[c]
  /\ approved' = [approved EXCEPT ![c] = FALSE]
  /\ UNCHANGED <<state, left, resumes>> /\ AutoUnchanged

\* ---- A person ----

\* approve and unapprove take the change lock: never while a run is active.
Approve(c) ==
  /\ state[c] # "active"
  /\ approved' = [approved EXCEPT ![c] = ~approved[c]]
  /\ UNCHANGED <<state, left, resumes>> /\ AutoUnchanged

\* tuckerbox run by hand (the run gate passes).
PersonStart(c) ==
  /\ state[c] \in {"new", "ready"}
  /\ approved[c]
  /\ state' = [state EXCEPT ![c] = "active"]
  /\ resumes' = [resumes EXCEPT ![c] = 0]
  /\ UNCHANGED <<approved, left>> /\ AutoUnchanged

PersonStop(c) ==
  /\ state[c] = "active"
  /\ state' = [state EXCEPT ![c] = "stopped"]
  /\ UNCHANGED <<approved, left, resumes>> /\ AutoUnchanged

\* tuckerbox resume (stopped, blocked or crashed): a new attempt.
PersonResume(c) ==
  /\ state[c] \in {"stopped", "blocked", "crashed"}
  /\ state' = [state EXCEPT ![c] = "active"]
  /\ resumes' = [resumes EXCEPT ![c] = 0]
  /\ UNCHANGED <<approved, left>> /\ AutoUnchanged

\* ---- Autopilot ----

\* A pass reads one change's run state and approval (not atomic with acting).
Look(c) ==
  /\ pc = "idle"
  /\ pc' = "read"
  /\ target' = c
  /\ seen' = [st |-> state[c], ap |-> approved[c]]
  /\ UNCHANGED <<state, approved, left, resumes, badStart, badResume>>

\* The engine's check under the change lock: the run is still as seen.
StillAsSeen(c) == state[c] = seen.st

WantsStart == seen.st \in {"new", "ready"} /\ seen.ap
WantsResume(c) == seen.st = "crashed" /\ resumes[c] < MaxAutoResumes

\* Start (or start the next group). The run gate always judges approval
\* under the lock, as add-herdr-board requires.
AutoStart ==
  LET c == target IN
  /\ pc = "read"
  /\ WantsStart
  /\ state[c] # "active"                 \* the change lock: one run per change
  /\ IF StillAsSeen(c) /\ approved[c]
       THEN /\ state' = [state EXCEPT ![c] = "active"]
            /\ resumes' = [resumes EXCEPT ![c] = 0]
            /\ badStart' = (badStart \/ state[c] \notin {"new", "ready"} \/ ~approved[c])
       ELSE UNCHANGED <<state, resumes, badStart>>
  /\ pc' = "idle"
  /\ UNCHANGED <<approved, left, target, seen, badResume>>

\* Resume a crashed run, once per crash.
AutoResume ==
  LET c == target IN
  /\ pc = "read"
  /\ WantsResume(c)
  /\ state[c] # "active"
  /\ IF StillAsSeen(c)
       THEN /\ state' = [state EXCEPT ![c] = "active"]
            /\ resumes' = [resumes EXCEPT ![c] = resumes[c] + 1]
            /\ badResume' = (badResume \/ state[c] # "crashed")
       ELSE UNCHANGED <<state, resumes, badResume>>
  /\ pc' = "idle"
  /\ UNCHANGED <<approved, left, target, seen, badStart>>

\* Nothing to do for what it saw, or the change lock is taken.
AutoSkip ==
  /\ pc = "read"
  /\ ~((WantsStart \/ WantsResume(target)) /\ state[target] # "active")
  /\ pc' = "idle"
  /\ UNCHANGED <<state, approved, left, resumes, target, seen, badStart, badResume>>

\* Autopilot exits and is started again: it forgets what it saw.
Restart ==
  /\ pc' = "idle"
  /\ UNCHANGED <<state, approved, left, resumes, target, seen, badStart, badResume>>

Next ==
  \/ \E c \in Changes :
       Finish(c) \/ Block(c) \/ Crash(c) \/ EditText(c)
       \/ Approve(c) \/ PersonStart(c) \/ PersonStop(c) \/ PersonResume(c) \/ Look(c)
  \/ AutoStart \/ AutoResume \/ AutoSkip \/ Restart

Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ state \in [Changes -> States]
  /\ approved \in [Changes -> BOOLEAN]
  /\ left \in [Changes -> 0..Groups]
  /\ resumes \in [Changes -> 0..MaxAutoResumes]
  /\ pc \in {"idle", "read"}

\* autopilot: Acting on what it saw. Every run autopilot starts was
\* approved and startable (no run, or ready) when it started.
ApprovedAtStart == ~badStart

\* autopilot: Autopilot never overrides a person. It resumes only crashed
\* runs, never a stopped or blocked one.
PersonStopsHold == ~badResume
=============================================================================
