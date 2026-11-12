---------------------------- MODULE autopilot ----------------------------
\* The autopilot pickup loop (openspec change add-autopilot).
\* Autopilot reads a change's run state (state and since, here an attempt
\* number) and approval, then later starts or resumes the run. A person and
\* the run itself change both in between. The run engine checks, under the
\* change lock, that the run is still the attempt autopilot saw, and the
\* approval rule: a start needs the change approved; a crash resume needs
\* it not unapproved (a stale approval from the run's own steps is fine).
\* Crash resumes are limited per crash streak, counted from history.
\* A change can be archived on the default branch some other way (merged by
\* hand); autopilot then closes its run if no agent runs (openspec change
\* fix-factory-gaps). A person can close a run by hand.
EXTENDS Naturals

CONSTANTS Changes, Groups, MaxAutoResumes, MaxAttempt

None == "none"

\* Run states. "stopped" is a stop by a person; "crashed" is a run whose
\* process died. "done" means every group ran and the run went on to ship.
\* "closed" is merged elsewhere or closed by a person.
States == {"new", "active", "ready", "blocked", "stopped", "crashed", "done", "closed"}

\* Runs with no agent running that may be closed.
Closable == {"ready", "blocked", "stopped", "crashed"}

Approvals == {"approved", "stale", "none"}

VARIABLES
  state,      \* run state per change, in run.json
  attempt,    \* run.json's since: changes on every state change
  approval,   \* approval state per change, from the files
  left,       \* task groups not yet ticked
  archived,   \* the change is archived on the fetched default branch
  streak,     \* autopilot resumes since the run's last progress or person action (history)
  pc,         \* autopilot: "idle" or "read"
  target,     \* the change autopilot looked at
  seen,       \* what it saw: [st, at, ap, ar]
  badStart,   \* autopilot started a change that was not approved or not startable
  badResume,  \* autopilot resumed a run that was not crashed, or was unapproved
  badStale,   \* autopilot acted on an attempt other than the one it saw
  badClose    \* autopilot closed a run with an agent, or of a change not archived

vars == <<state, attempt, approval, left, archived, streak, pc, target, seen,
          badStart, badResume, badStale, badClose>>

Init ==
  /\ state = [c \in Changes |-> "new"]
  /\ attempt = [c \in Changes |-> 0]
  /\ approval \in [Changes -> {"approved", "none"}]
  /\ left = [c \in Changes |-> Groups]
  /\ archived = [c \in Changes |-> FALSE]
  /\ streak = [c \in Changes |-> 0]
  /\ pc = "idle"
  /\ target = None
  /\ seen = [st |-> "new", at |-> 0, ap |-> "none", ar |-> FALSE]
  /\ badStart = FALSE
  /\ badResume = FALSE
  /\ badStale = FALSE
  /\ badClose = FALSE

\* Every run.json write of a new state gets a new since.
Move(c, s) ==
  /\ state' = [state EXCEPT ![c] = s]
  /\ attempt' = [attempt EXCEPT ![c] = attempt[c] + 1]

\* Autopilot's variables, and the archive, which only the actions below change.
AutoUnchanged == UNCHANGED <<pc, target, seen, badStart, badResume, badStale, badClose, archived>>

\* ---- The run (engine), one group per run attempt ----

\* A group finishes: progress, so the crash streak ends.
Finish(c) ==
  /\ state[c] = "active"
  /\ left' = [left EXCEPT ![c] = left[c] - 1]
  /\ Move(c, IF left[c] = 1 THEN "done" ELSE "ready")
  /\ streak' = [streak EXCEPT ![c] = 0]
  /\ UNCHANGED approval /\ AutoUnchanged

Block(c) ==
  /\ state[c] = "active"
  /\ Move(c, "blocked")
  /\ streak' = [streak EXCEPT ![c] = 0]
  /\ UNCHANGED <<approval, left>> /\ AutoUnchanged

Crash(c) ==
  /\ state[c] = "active"
  /\ Move(c, "crashed")
  /\ UNCHANGED <<approval, left, streak>> /\ AutoUnchanged

\* A step edits the change's text: the approval goes stale mid-run.
EditText(c) ==
  /\ state[c] = "active"
  /\ approval[c] = "approved"
  /\ approval' = [approval EXCEPT ![c] = "stale"]
  /\ UNCHANGED <<state, attempt, left, streak>> /\ AutoUnchanged

\* ---- A person ----

\* approve and unapprove take the change lock: never while a run is active.
Approve(c) ==
  /\ state[c] # "active"
  /\ \E a \in {"approved", "none"} : a # approval[c] /\ approval' = [approval EXCEPT ![c] = a]
  /\ UNCHANGED <<state, attempt, left, streak>> /\ AutoUnchanged

\* tuckerbox run by hand (the run gate passes).
PersonStart(c) ==
  /\ state[c] \in {"new", "ready", "closed"}
  /\ approval[c] = "approved"
  /\ ~archived[c]
  /\ Move(c, "active")
  /\ streak' = [streak EXCEPT ![c] = 0]
  /\ UNCHANGED <<approval, left>> /\ AutoUnchanged

PersonStop(c) ==
  /\ state[c] = "active"
  /\ Move(c, "stopped")
  /\ UNCHANGED <<approval, left, streak>> /\ AutoUnchanged

\* tuckerbox resume (stopped, blocked or crashed): the streak starts again.
PersonResume(c) ==
  /\ state[c] \in {"stopped", "blocked", "crashed"}
  /\ Move(c, "active")
  /\ streak' = [streak EXCEPT ![c] = 0]
  /\ UNCHANGED <<approval, left>> /\ AutoUnchanged

\* tuckerbox close: a run with no agent running.
PersonClose(c) ==
  /\ state[c] \in Closable
  /\ Move(c, "closed")
  /\ UNCHANGED <<approval, left, streak>> /\ AutoUnchanged

\* ---- Someone else ----

\* The change is archived on the default branch (merged by hand), at any
\* time, even while its run is active.
ArchiveElsewhere(c) ==
  /\ ~archived[c]
  /\ archived' = [archived EXCEPT ![c] = TRUE]
  /\ UNCHANGED <<state, attempt, approval, left, streak, pc, target, seen,
                 badStart, badResume, badStale, badClose>>

\* ---- Autopilot ----

\* A pass reads one change's run.json and approval (not atomic with acting).
Look(c) ==
  /\ pc = "idle"
  /\ pc' = "read"
  /\ target' = c
  /\ seen' = [st |-> state[c], at |-> attempt[c], ap |-> approval[c], ar |-> archived[c]]
  /\ UNCHANGED <<state, attempt, approval, left, archived, streak, badStart, badResume, badStale, badClose>>

\* The engine's checks, under the change lock.
StillAsSeen(c) == state[c] = seen.st /\ attempt[c] = seen.at
StartGate(c) == approval[c] = "approved" /\ ~archived[c]
ResumeGate(c) == approval[c] # "none" /\ ~archived[c]
CloseGate(c) == StillAsSeen(c) /\ archived[c]
Budget(c) == streak[c] < MaxAutoResumes

WantsStart == seen.st \in {"new", "ready"} /\ seen.ap = "approved" /\ ~seen.ar
WantsResume == seen.st = "crashed" /\ seen.ap # "none" /\ ~seen.ar
WantsClose == seen.st \in Closable /\ seen.ar

\* Start (or start the next group).
AutoStart ==
  LET c == target IN
  /\ pc = "read"
  /\ WantsStart
  /\ state[c] # "active"                 \* the change lock: one run per change
  /\ IF StillAsSeen(c) /\ StartGate(c)
       THEN /\ Move(c, "active")
            /\ streak' = [streak EXCEPT ![c] = 0]
            /\ badStart' = (badStart \/ state[c] \notin {"new", "ready"} \/ approval[c] # "approved"
                             \/ archived[c])
            /\ badStale' = (badStale \/ attempt[c] # seen.at)
       ELSE UNCHANGED <<state, attempt, streak, badStart, badStale>>
  /\ pc' = "idle"
  /\ UNCHANGED <<approval, left, archived, target, seen, badResume, badClose>>

\* Resume a crashed run, within the crash budget.
AutoResume ==
  LET c == target IN
  /\ pc = "read"
  /\ WantsResume
  /\ state[c] # "active"
  /\ IF StillAsSeen(c) /\ ResumeGate(c) /\ Budget(c)
       THEN /\ Move(c, "active")
            /\ streak' = [streak EXCEPT ![c] = streak[c] + 1]
            /\ badResume' = (badResume \/ state[c] # "crashed" \/ approval[c] = "none" \/ archived[c])
            /\ badStale' = (badStale \/ attempt[c] # seen.at)
       ELSE UNCHANGED <<state, attempt, streak, badResume, badStale>>
  /\ pc' = "idle"
  /\ UNCHANGED <<approval, left, archived, target, seen, badStart, badClose>>

\* Close the run of a change archived elsewhere, if no agent runs.
AutoClose ==
  LET c == target IN
  /\ pc = "read"
  /\ WantsClose
  /\ state[c] # "active"
  /\ IF CloseGate(c)
       THEN /\ Move(c, "closed")
            /\ badClose' = (badClose \/ state[c] \notin Closable \/ ~archived[c])
            /\ badStale' = (badStale \/ attempt[c] # seen.at)
       ELSE UNCHANGED <<state, attempt, badClose, badStale>>
  /\ pc' = "idle"
  /\ UNCHANGED <<approval, left, archived, streak, target, seen, badStart, badResume>>

\* Nothing to do for what it saw, or the change lock is taken.
AutoSkip ==
  /\ pc = "read"
  /\ ~((WantsStart \/ WantsResume \/ WantsClose) /\ state[target] # "active")
  /\ pc' = "idle"
  /\ UNCHANGED <<state, attempt, approval, left, archived, streak, target, seen,
                 badStart, badResume, badStale, badClose>>

\* Autopilot exits and is started again: it forgets what it saw. The
\* streak is in history, so it survives.
Restart ==
  /\ pc' = "idle"
  /\ UNCHANGED <<state, attempt, approval, left, archived, streak, target, seen,
                 badStart, badResume, badStale, badClose>>

Next ==
  \/ \E c \in Changes :
       Finish(c) \/ Block(c) \/ Crash(c) \/ EditText(c) \/ Approve(c)
       \/ PersonStart(c) \/ PersonStop(c) \/ PersonResume(c) \/ PersonClose(c)
       \/ ArchiveElsewhere(c) \/ Look(c)
  \/ AutoStart \/ AutoResume \/ AutoClose \/ AutoSkip \/ Restart

Spec == Init /\ [][Next]_vars

\* Keeps the state space finite: since only grows.
AttemptBound == \A c \in Changes : attempt[c] <= MaxAttempt

TypeOK ==
  /\ state \in [Changes -> States]
  /\ approval \in [Changes -> Approvals]
  /\ left \in [Changes -> 0..Groups]
  /\ archived \in [Changes -> BOOLEAN]
  /\ streak \in [Changes -> Nat]
  /\ pc \in {"idle", "read"}

\* autopilot: Acting on what it saw. Every start or next group autopilot
\* makes is of an approved change with no run, or a ready run.
ApprovedAtStart == ~badStart

\* autopilot: Autopilot never overrides a person. It resumes only crashed
\* runs, never a stopped or blocked one, nor one a person unapproved.
PersonStopsHold == ~badResume

\* autopilot: Acting on what it saw. It acts only on the attempt it read
\* (state and since), not on a later one that looks the same.
ActsOnSeenAttempt == ~badStale

\* autopilot: Crashed runs. Autopilot resumes at most MaxAutoResumes times
\* per crash streak, whatever the rounds.
CrashBudget == \A c \in Changes : streak[c] <= MaxAutoResumes

\* run-closing: Runs of changes merged elsewhere. Autopilot closes only a
\* run with no agent running whose change is archived.
ClosesOnlyIdleArchived == ~badClose
=============================================================================
