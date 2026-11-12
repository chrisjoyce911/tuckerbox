---------------------------- MODULE approvals ----------------------------
\* Publishing approvals (openspec change fix-approval-publish). Approvals live
\* on one ref on the remote, refs/tuckerbox/approvals: a commit holding one
\* file per approved change. `tuckerbox approve` and `unapprove`, run by people
\* on several machines, each fetch the ref, change one change's file, and push
\* a new commit with --force-with-lease on the version they fetched. A refused
\* push fetches and tries again.
EXTENDS Naturals

CONSTANTS Machines, Changes, MaxVersion

VARIABLES
  version,  \* the ref's commit, as a counter
  ref,      \* ref[c]: is change c approved on the remote
  pc,       \* each machine's stage
  job,      \* the change and value each machine is publishing
  seen,     \* the version each machine fetched
  snap,     \* the approvals each machine fetched
  want      \* ghost: the last value accepted for each change
vars == <<version, ref, pc, job, seen, snap, want>>

Ops == [c : Changes, v : BOOLEAN]

Init ==
  /\ version = 0
  /\ ref = [c \in Changes |-> FALSE]
  /\ pc = [m \in Machines |-> "idle"]
  /\ job \in [Machines -> Ops]
  /\ seen = [m \in Machines |-> 0]
  /\ snap = [m \in Machines |-> [c \in Changes |-> FALSE]]
  /\ want = [c \in Changes |-> FALSE]

\* A person runs approve or unapprove on machine m for some change.
Begin(m) ==
  /\ pc[m] = "idle"
  /\ \E o \in Ops : job' = [job EXCEPT ![m] = o]
  /\ pc' = [pc EXCEPT ![m] = "fetch"]
  /\ UNCHANGED <<version, ref, seen, snap, want>>

Fetch(m) ==
  /\ pc[m] = "fetch"
  /\ seen' = [seen EXCEPT ![m] = version]
  /\ snap' = [snap EXCEPT ![m] = ref]
  /\ pc' = [pc EXCEPT ![m] = "push"]
  /\ UNCHANGED <<version, ref, job, want>>

\* Push a commit built from the fetched approvals with this one change set.
Lease(m) == version = seen[m]

Push(m) ==
  /\ pc[m] = "push"
  /\ version < MaxVersion
  /\ IF Lease(m)
       THEN /\ ref' = [snap[m] EXCEPT ![job[m].c] = job[m].v]
            /\ version' = version + 1
            /\ want' = [want EXCEPT ![job[m].c] = job[m].v]
            /\ pc' = [pc EXCEPT ![m] = "idle"]
       ELSE /\ pc' = [pc EXCEPT ![m] = "fetch"]   \* refused: fetch and try again
            /\ UNCHANGED <<ref, version, want>>
  /\ UNCHANGED <<job, seen, snap>>

Finished ==
  /\ version = MaxVersion
  /\ UNCHANGED vars

Next == (\E m \in Machines : Begin(m) \/ Fetch(m) \/ Push(m)) \/ Finished

Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ version \in 0..MaxVersion
  /\ ref \in [Changes -> BOOLEAN]
  /\ pc \in [Machines -> {"idle", "fetch", "push"}]

\* approval-publish: no accepted approve or unapprove is lost: the ref always
\* holds, for each change, the last value a push was accepted for.
NoLostApproval == ref = want
=============================================================================
