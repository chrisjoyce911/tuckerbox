---------------------------- MODULE fleet_broken_plainclaim ----------------------------
\* Fleet mode (openspec change add-operating-modes): several of one owner's
\* machines pick up approved changes from the same repository.
\* The shared state is one git ref on the repository's remote, the fleet ref,
\* holding a claim per change: the holder, an epoch, and when it was renewed.
\* The only atomic primitive is a push with --force-with-lease, a
\* compare-and-swap on a ref. A push may name several refs with --atomic, so
\* the first push of a change's branch and a lease on the fleet ref succeed or
\* fail together.
\* A claim expires when its holder stops renewing it (a crash, a sleeping
\* laptop, a lost network). Another machine may then take it over. The old
\* holder may come back without knowing.
EXTENDS Naturals, FiniteSets

CONSTANTS Machines, Changes, MaxRuns, MaxEpoch, MaxVersion

None == "none"
NoClaim == [holder |-> None, epoch |-> 0]

VARIABLES
  version,  \* the fleet ref's commit, as a counter: each accepted push makes a new one
  claim,    \* claim[c]: the holder and epoch the fleet ref records for change c
  expired,  \* expired[c]: the claim on c was not renewed in time
  pc,       \* each machine's stage
  seen,     \* the fleet ref version each machine last fetched
  snap,     \* the claims each machine last fetched
  job,      \* the change each machine works on
  epoch,    \* the epoch each machine believes it holds
  pushed,   \* pushed[c]: <<machine, epoch>> whose push created c's branch, or None
  grants,   \* every <<change, epoch, machine>> a claim push handed out
  stale     \* a branch push was accepted from a machine that no longer held the claim
vars == <<version, claim, expired, pc, seen, snap, job, epoch, pushed, grants, stale>>

Init ==
  /\ version = 0
  /\ claim = [c \in Changes |-> NoClaim]
  /\ expired = [c \in Changes |-> FALSE]
  /\ pc = [m \in Machines |-> "idle"]
  /\ seen = [m \in Machines |-> 0]
  /\ snap = [m \in Machines |-> [c \in Changes |-> NoClaim]]
  /\ job = [m \in Machines |-> None]
  /\ epoch = [m \in Machines |-> 0]
  /\ pushed = [c \in Changes |-> None]
  /\ grants = {}
  /\ stale = FALSE

Held(cl, c) == cl[c].holder # None
Count(cl) == Cardinality({c \in Changes : Held(cl, c)})
Holds(m) == job[m] # None /\ claim[job[m]].holder = m /\ claim[job[m]].epoch = epoch[m]

\* `git fetch` of the fleet ref.
Fetch(m) ==
  /\ pc[m] = "idle"
  /\ seen' = [seen EXCEPT ![m] = version]
  /\ snap' = [snap EXCEPT ![m] = claim]
  /\ pc' = [pc EXCEPT ![m] = "fetched"]
  /\ UNCHANGED <<version, claim, expired, job, epoch, pushed, grants, stale>>

\* Claim a change that is free, or take over one whose claim expired, judged
\* from the fetched copy. The push carries --force-with-lease=<fleet ref>:<seen>,
\* so it is refused if anyone pushed the fleet ref since the fetch. A refused
\* push fetches again. Taking over keeps the number of claims the same.
Claim(m, c) ==
  /\ pc[m] = "fetched"
  /\ ~Held(snap[m], c) \/ expired[c]
  /\ Held(snap[m], c) \/ Count(snap[m]) < MaxRuns
  /\ snap[m][c].epoch < MaxEpoch
  /\ version < MaxVersion
  /\ IF TRUE \* BROKEN: plain push of the fleet ref, no --force-with-lease
       THEN LET e == snap[m][c].epoch + 1 IN
            /\ claim' = [snap[m] EXCEPT ![c] = [holder |-> m, epoch |-> e]]
            /\ expired' = [expired EXCEPT ![c] = FALSE]
            /\ version' = version + 1
            /\ grants' = grants \cup {<<c, e, m>>}
            /\ job' = [job EXCEPT ![m] = c]
            /\ epoch' = [epoch EXCEPT ![m] = e]
            /\ pc' = [pc EXCEPT ![m] = "running"]
       ELSE /\ pc' = [pc EXCEPT ![m] = "idle"]
            /\ UNCHANGED <<claim, expired, version, grants, job, epoch>>
  /\ UNCHANGED <<seen, snap, pushed, stale>>

\* Nothing to claim in the fetched copy: try again later.
Pass(m) ==
  /\ pc[m] = "fetched"
  /\ pc' = [pc EXCEPT ![m] = "idle"]
  /\ UNCHANGED <<version, claim, expired, seen, snap, job, epoch, pushed, grants, stale>>

\* The holder renews its claim. A renewal is fetch, check, then a lease push of
\* the fleet ref; a push refused because another claim moved the ref fetches and
\* checks again. So it acts on the current ref. A machine that finds the claim
\* is no longer its own stops: it is blocked and pushes nothing.
Renew(m) ==
  /\ pc[m] \in {"running", "shipped"}
  /\ version < MaxVersion
  /\ IF Holds(m)
       THEN /\ version' = version + 1
            /\ expired' = [expired EXCEPT ![job[m]] = FALSE]
            /\ UNCHANGED pc
       ELSE /\ pc' = [pc EXCEPT ![m] = "lost"]
            /\ UNCHANGED <<version, expired>>
  /\ UNCHANGED <<claim, seen, snap, job, epoch, pushed, grants, stale>>

\* Time passes without a renewal. Nothing on the remote changes: expiry is read
\* from the renewal time in the claim.
Expire(c) ==
  /\ Held(claim, c)
  /\ ~expired[c]
  /\ expired' = [expired EXCEPT ![c] = TRUE]
  /\ UNCHANGED <<version, claim, pc, seen, snap, job, epoch, pushed, grants, stale>>

\* The run's process dies or its machine sleeps, and later carries on, unaware
\* of anything that happened meanwhile.
Crash(m) ==
  /\ pc[m] = "running"
  /\ pc' = [pc EXCEPT ![m] = "crashed"]
  /\ UNCHANGED <<version, claim, expired, seen, snap, job, epoch, pushed, grants, stale>>

Wake(m) ==
  /\ pc[m] = "crashed"
  /\ pc' = [pc EXCEPT ![m] = "running"]
  /\ UNCHANGED <<version, claim, expired, seen, snap, job, epoch, pushed, grants, stale>>

\* The ship stage's first push of the change's branch (add-forge-shipping:
\* lease on the branch, expecting none). In fleet mode the same atomic push
\* carries a lease on the fleet ref, so it is accepted only while this machine
\* still holds the claim.
PushGuard(m) == pushed[job[m]] = None /\ Holds(m)

Ship(m) ==
  /\ pc[m] = "running"
  /\ IF PushGuard(m)
       THEN /\ pushed' = [pushed EXCEPT ![job[m]] = <<m, epoch[m]>>]
            /\ stale' = (stale \/ ~Holds(m))
            /\ pc' = [pc EXCEPT ![m] = "shipped"]
       ELSE /\ pc' = [pc EXCEPT ![m] = "lost"]
            /\ UNCHANGED <<pushed, stale>>
  /\ UNCHANGED <<version, claim, expired, seen, snap, job, epoch, grants>>

\* The run ends (ready or merged) and releases its claim, if it is still its own.
Release(m) ==
  /\ pc[m] = "shipped"
  /\ version < MaxVersion
  /\ IF Holds(m)
       THEN /\ claim' = [claim EXCEPT ![job[m]] = [holder |-> None, epoch |-> epoch[m]]]
            /\ version' = version + 1
       ELSE UNCHANGED <<claim, version>>
  /\ pc' = [pc EXCEPT ![m] = "done"]
  /\ UNCHANGED <<expired, seen, snap, job, epoch, pushed, grants, stale>>

\* Every machine has stopped, or the bound on fleet ref pushes is used up:
\* nothing more happens, and TLC does not report the bound as a deadlock.
Finished ==
  /\ \/ \A m \in Machines : pc[m] \in {"done", "lost"}
     \/ version = MaxVersion
  /\ UNCHANGED vars

Next ==
  \/ \E m \in Machines :
       \/ Fetch(m) \/ Pass(m) \/ Renew(m) \/ Crash(m) \/ Wake(m) \/ Ship(m) \/ Release(m)
       \/ \E c \in Changes : Claim(m, c)
  \/ \E c \in Changes : Expire(c)
  \/ Finished

Spec == Init /\ [][Next]_vars

ClaimType == [holder : Machines \cup {None}, epoch : 0..MaxEpoch]

TypeOK ==
  /\ version \in 0..MaxVersion
  /\ claim \in [Changes -> ClaimType]
  /\ expired \in [Changes -> BOOLEAN]
  /\ pc \in [Machines -> {"idle", "fetched", "running", "crashed", "shipped", "done", "lost"}]
  /\ seen \in [Machines -> 0..MaxVersion]
  /\ snap \in [Machines -> [Changes -> ClaimType]]
  /\ job \in [Machines -> Changes \cup {None}]
  /\ epoch \in [Machines -> 0..MaxEpoch]
  /\ stale \in BOOLEAN

\* fleet-claims: each claim on a change, first or a take-over, goes to exactly
\* one machine.
OneHolderPerEpoch ==
  \A g, h \in grants : (g[1] = h[1] /\ g[2] = h[2]) => g[3] = h[3]

\* fleet-claims: a machine whose claim was taken over never pushes the
\* change's branch.
FencedPush == ~stale

\* fleet-claims: the fleet ref never records more claims than the repository's
\* limit.
WithinFleetLimit == Count(claim) <= MaxRuns
=============================================================================
