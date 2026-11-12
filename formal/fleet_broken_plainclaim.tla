---------------------------- MODULE fleet_broken_plainclaim ----------------------------
\* Fleet mode (openspec change add-operating-modes): several of one owner's
\* machines pick up approved changes from the same repository.
\* Shared state on the git remote and its forge:
\*   - the fleet ref: a claim per change (holder, epoch, renewal time)
\*   - each change's branch tip
\*   - whether the change's merge request was merged
\* Atomic primitives:
\*   - a push with --force-with-lease (compare-and-swap on a ref), and with
\*     --atomic on several refs at once: in fleet mode every push of a change's
\*     branch also leases and renews the fleet ref
\*   - a merge pinned to a commit: the forge refuses it if the branch tip moved
\* A claim expires when it is not renewed in time; another machine may then take
\* it over. A machine that is slow, asleep or cut off is one whose actions are
\* not scheduled for a while: interleaving covers every such delay, at any stage,
\* so no separate crash action is needed. A machine that never comes back simply
\* never acts again.
EXTENDS Naturals, FiniteSets

CONSTANTS Machines, Changes, MaxRuns, MaxEpoch, MaxVersion, MaxCommits

None == "none"
NoClaim == [holder |-> None, epoch |-> 0, ready |-> FALSE]

VARIABLES
  version,    \* the fleet ref's commit, as a counter
  claim,      \* claim[c]: holder, epoch and whether the run is ready (a finished scope)
  expired,    \* expired[c]: the claim on c was not renewed in time
  tip,        \* tip[c]: the change branch's tip commit (0: no branch)
  next,       \* the next unused commit id
  merged,     \* merged[c]: the forge merged c's merge request
  pc,         \* each machine's stage
  seen,       \* the fleet ref version each machine last fetched
  snap,       \* the claims each machine last fetched
  snapTip,    \* the branch tips each machine last fetched
  job,        \* the change each machine works on
  epoch,      \* the epoch each machine believes it holds
  lease,      \* the branch tip each machine expects (its last push)
  checked,    \* the commit each machine saw green and will merge
  grants,     \* every <<change, epoch, machine>> a claim push handed out
  stalePush,  \* a branch push was accepted from a machine not holding the claim
  staleMerge  \* a merge was accepted from a machine not holding the claim
vars == <<version, claim, expired, tip, next, merged, pc, seen, snap, snapTip,
          job, epoch, lease, checked, grants, stalePush, staleMerge>>

Init ==
  /\ version = 0
  /\ claim = [c \in Changes |-> NoClaim]
  /\ expired = [c \in Changes |-> FALSE]
  /\ tip = [c \in Changes |-> 0]
  /\ next = 1
  /\ merged = [c \in Changes |-> FALSE]
  /\ pc = [m \in Machines |-> "idle"]
  /\ seen = [m \in Machines |-> 0]
  /\ snap = [m \in Machines |-> [c \in Changes |-> NoClaim]]
  /\ snapTip = [m \in Machines |-> [c \in Changes |-> 0]]
  /\ job = [m \in Machines |-> None]
  /\ epoch = [m \in Machines |-> 0]
  /\ lease = [m \in Machines |-> 0]
  /\ checked = [m \in Machines |-> 0]
  /\ grants = {}
  /\ stalePush = FALSE
  /\ staleMerge = FALSE

Held(cl, c) == cl[c].holder # None
\* Ready claims don't count toward the fleet limit; active and parked ones do.
Count(cl) == Cardinality({c \in Changes : Held(cl, c) /\ ~cl[c].ready})
Holds(m) == job[m] # None /\ claim[job[m]].holder = m /\ claim[job[m]].epoch = epoch[m]

\* `git fetch` of the fleet ref and the change branches.
Fetch(m) ==
  /\ pc[m] = "idle"
  /\ seen' = [seen EXCEPT ![m] = version]
  /\ snap' = [snap EXCEPT ![m] = claim]
  /\ snapTip' = [snapTip EXCEPT ![m] = tip]
  /\ pc' = [pc EXCEPT ![m] = "fetched"]
  /\ UNCHANGED <<version, claim, expired, tip, next, merged, job, epoch, lease,
                 checked, grants, stalePush, staleMerge>>

\* Claim a free change, or take over one whose claim expired, judged from the
\* fetched copy. One atomic push leases the fleet ref at the fetched version
\* and the change's branch at the fetched tip. When the branch exists (the old
\* holder was shipping), the push also moves it to a new take-over commit, so
\* the old holder's branch lease and its pinned merge both stop matching.
TakeoverTip(c) == next

Claim(m, c) ==
  /\ pc[m] = "fetched"
  /\ ~merged[c]
  /\ ~Held(snap[m], c) \/ expired[c]
  /\ Held(snap[m], c) \/ Count(snap[m]) < MaxRuns
  /\ snap[m][c].epoch < MaxEpoch
  /\ version < MaxVersion
  /\ snapTip[m][c] # 0 => next <= MaxCommits
  /\ IF TRUE \* BROKEN: plain pushes, no --force-with-lease on the claim
       THEN LET e == snap[m][c].epoch + 1
                t == IF tip[c] = 0 THEN 0 ELSE TakeoverTip(c) IN
            /\ claim' = [snap[m] EXCEPT ![c] = [holder |-> m, epoch |-> e, ready |-> FALSE]]
            /\ expired' = [expired EXCEPT ![c] = FALSE]
            /\ version' = version + 1
            /\ grants' = grants \cup {<<c, e, m>>}
            /\ job' = [job EXCEPT ![m] = c]
            /\ epoch' = [epoch EXCEPT ![m] = e]
            /\ tip' = [tip EXCEPT ![c] = t]
            /\ next' = IF tip[c] = 0 THEN next ELSE next + 1
            /\ lease' = [lease EXCEPT ![m] = t]
            /\ pc' = [pc EXCEPT ![m] = IF tip[c] = 0 THEN "running" ELSE "shipping"]
       ELSE /\ pc' = [pc EXCEPT ![m] = "idle"]
            /\ UNCHANGED <<claim, expired, version, grants, job, epoch, tip, next, lease>>
  /\ UNCHANGED <<merged, seen, snap, snapTip, checked, stalePush, staleMerge>>

\* Nothing to claim in the fetched copy: try again later.
Pass(m) ==
  /\ pc[m] = "fetched"
  /\ pc' = [pc EXCEPT ![m] = "idle"]
  /\ UNCHANGED <<version, claim, expired, tip, next, merged, seen, snap, snapTip,
                 job, epoch, lease, checked, grants, stalePush, staleMerge>>

\* The holder renews its claim (fetch, check, lease push; a refusal caused by
\* another claim's push fetches and checks again, so it acts on the current
\* ref). A machine that finds the claim is not its own stops and is blocked.
Renew(m) ==
  /\ pc[m] \in {"running", "shipping", "merging"}
  /\ version < MaxVersion
  /\ IF Holds(m)
       THEN /\ version' = version + 1
            /\ expired' = [expired EXCEPT ![job[m]] = FALSE]
            /\ UNCHANGED pc
       ELSE /\ pc' = [pc EXCEPT ![m] = "lost"]
            /\ UNCHANGED <<version, expired>>
  /\ UNCHANGED <<claim, tip, next, merged, seen, snap, snapTip, job, epoch, lease,
                 checked, grants, stalePush, staleMerge>>

\* Time passes without a renewal. Expiry is read from the renewal time.
Expire(c) ==
  /\ Held(claim, c)
  /\ ~claim[c].ready
  /\ ~expired[c]
  /\ expired' = [expired EXCEPT ![c] = TRUE]
  /\ UNCHANGED <<version, claim, tip, next, merged, pc, seen, snap, snapTip, job,
                 epoch, lease, checked, grants, stalePush, staleMerge>>

\* Any push of the change's branch: the first one at the ship stage, or a fix
\* round's. One atomic push with a lease on the branch (the tip this machine
\* last pushed, or none) and a lease on the fleet ref that renews this
\* machine's claim. Refused if either lease fails: the run is then blocked.
PushGuard(m) == tip[job[m]] = lease[m] /\ Holds(m)

Push(m) ==
  /\ pc[m] \in {"running", "shipping"}
  /\ next <= MaxCommits
  /\ version < MaxVersion
  /\ IF PushGuard(m)
       THEN /\ stalePush' = (stalePush \/ ~Holds(m))
            /\ tip' = [tip EXCEPT ![job[m]] = next]
            /\ lease' = [lease EXCEPT ![m] = next]
            /\ next' = next + 1
            /\ version' = version + 1
            /\ expired' = [expired EXCEPT ![job[m]] = FALSE]
            /\ pc' = [pc EXCEPT ![m] = "shipping"]
       ELSE /\ pc' = [pc EXCEPT ![m] = "lost"]
            /\ UNCHANGED <<stalePush, tip, lease, next, version, expired>>
  /\ UNCHANGED <<claim, merged, seen, snap, snapTip, job, epoch, checked, grants, staleMerge>>

\* The run reads CI for the commit it last pushed and sees it green (merge
\* mode). Reading CI and merging are separate steps: anything may happen
\* in between.
SeeGreen(m) ==
  /\ pc[m] = "shipping"
  /\ lease[m] # 0
  /\ checked' = [checked EXCEPT ![m] = lease[m]]
  /\ pc' = [pc EXCEPT ![m] = "merging"]
  /\ UNCHANGED <<version, claim, expired, tip, next, merged, seen, snap, snapTip,
                 job, epoch, lease, grants, stalePush, staleMerge>>

\* A merge pinned to the commit seen green: the forge refuses it if the tip
\* moved. A refused merge blocks the run.
Merge(m) ==
  /\ pc[m] = "merging"
  /\ IF tip[job[m]] = checked[m] /\ ~merged[job[m]]
       THEN /\ merged' = [merged EXCEPT ![job[m]] = TRUE]
            /\ staleMerge' = (staleMerge \/ ~Holds(m))
            /\ pc' = [pc EXCEPT ![m] = "merged"]
       ELSE /\ pc' = [pc EXCEPT ![m] = "lost"]
            /\ UNCHANGED <<merged, staleMerge>>
  /\ UNCHANGED <<version, claim, expired, tip, next, seen, snap, snapTip, job,
                 epoch, lease, checked, grants, stalePush>>

\* After the merge, the run removes its claim if it is still its own.
Release(m) ==
  /\ pc[m] = "merged"
  /\ version < MaxVersion
  /\ IF Holds(m)
       THEN /\ claim' = [claim EXCEPT ![job[m]] = [holder |-> None, epoch |-> epoch[m], ready |-> FALSE]]
            /\ version' = version + 1
       ELSE UNCHANGED <<claim, version>>
  /\ pc' = [pc EXCEPT ![m] = "done"]
  /\ UNCHANGED <<expired, tip, next, merged, seen, snap, snapTip, job, epoch, lease,
                 checked, grants, stalePush, staleMerge>>

\* A scoped run finishes a scope that doesn't complete the change: it is
\* ready and pushes nothing (add-task-scope). Its claim becomes ready, which
\* no longer counts toward the fleet limit, so other changes may use that room.
BecomeReady(m) ==
  /\ pc[m] = "running"
  /\ version < MaxVersion
  /\ IF Holds(m)
       THEN /\ claim' = [claim EXCEPT ![job[m]].ready = TRUE]
            /\ version' = version + 1
            /\ expired' = [expired EXCEPT ![job[m]] = FALSE]
            /\ pc' = [pc EXCEPT ![m] = "ready"]
       ELSE /\ pc' = [pc EXCEPT ![m] = "lost"]
            /\ UNCHANGED <<claim, version, expired>>
  /\ UNCHANGED <<tip, next, merged, seen, snap, snapTip, job, epoch, lease,
                 checked, grants, stalePush, staleMerge>>

\* `tuckerbox run <change> --tasks <next>` on the holder's machine: the ready
\* claim becomes active again, same holder and epoch. Like a renewal it acts
\* on the current ref (fetch, check, lease push, retry), and the fleet limit
\* is checked in the same compare-and-swap. Refused while the limit is full:
\* the run and its claim stay ready, so the action is not enabled then.
ScopeAdmit == Count(claim) < MaxRuns

NextScope(m) ==
  /\ pc[m] = "ready"
  /\ version < MaxVersion
  /\ ~Holds(m) \/ ScopeAdmit
  /\ IF Holds(m)
       THEN /\ claim' = [claim EXCEPT ![job[m]].ready = FALSE]
            /\ version' = version + 1
            /\ expired' = [expired EXCEPT ![job[m]] = FALSE]
            /\ pc' = [pc EXCEPT ![m] = "running"]
       ELSE /\ pc' = [pc EXCEPT ![m] = "lost"]
            /\ UNCHANGED <<claim, version, expired>>
  /\ UNCHANGED <<tip, next, merged, seen, snap, snapTip, job, epoch, lease,
                 checked, grants, stalePush, staleMerge>>

\* A person runs `tuckerbox claim release --force` on machine r for change c,
\* at any moment, also while the holder is shipping. Like a take-over, it is
\* one atomic push: it leases the fleet ref and removes the claim, and when the
\* branch exists it leases the branch at the fetched tip and moves it to a new
\* release commit, so the old holder's pushes and pinned merge stop matching.
ReleaseTip(c) == next

ForceRelease(r, c) ==
  /\ pc[r] = "fetched"
  /\ Held(snap[r], c)
  /\ ~merged[c]
  /\ version < MaxVersion
  /\ snapTip[r][c] # 0 => next <= MaxCommits
  /\ IF version = seen[r] /\ tip[c] = snapTip[r][c]
       THEN /\ claim' = [snap[r] EXCEPT ![c] = [holder |-> None, epoch |-> snap[r][c].epoch, ready |-> FALSE]]
            /\ version' = version + 1
            /\ tip' = [tip EXCEPT ![c] = IF tip[c] = 0 THEN 0 ELSE ReleaseTip(c)]
            /\ next' = IF tip[c] = 0 THEN next ELSE next + 1
       ELSE UNCHANGED <<claim, version, tip, next>>
  /\ pc' = [pc EXCEPT ![r] = "idle"]
  /\ UNCHANGED <<expired, merged, seen, snap, snapTip, job, epoch, lease, checked,
                 grants, stalePush, staleMerge>>

\* Every machine has stopped, or the bound on fleet ref pushes is used up:
\* nothing more happens, and TLC does not report the bound as a deadlock.
Finished ==
  /\ \/ \A m \in Machines : pc[m] \in {"done", "lost"}
     \/ version = MaxVersion
  /\ UNCHANGED vars

Next ==
  \/ \E m \in Machines :
       \/ Fetch(m) \/ Pass(m) \/ Renew(m) \/ Push(m) \/ SeeGreen(m) \/ Merge(m) \/ Release(m)
       \/ BecomeReady(m) \/ NextScope(m)
       \/ \E c \in Changes : Claim(m, c) \/ ForceRelease(m, c)
  \/ \E c \in Changes : Expire(c)
  \/ Finished

Spec == Init /\ [][Next]_vars

ClaimType == [holder : Machines \cup {None}, epoch : 0..MaxEpoch, ready : BOOLEAN]

TypeOK ==
  /\ version \in 0..MaxVersion
  /\ claim \in [Changes -> ClaimType]
  /\ expired \in [Changes -> BOOLEAN]
  /\ tip \in [Changes -> 0..MaxCommits]
  /\ next \in 1..(MaxCommits + 1)
  /\ merged \in [Changes -> BOOLEAN]
  /\ pc \in [Machines -> {"idle", "fetched", "running", "ready", "shipping", "merging", "merged", "done", "lost"}]
  /\ job \in [Machines -> Changes \cup {None}]
  /\ epoch \in [Machines -> 0..MaxEpoch]
  /\ lease \in [Machines -> 0..MaxCommits]
  /\ checked \in [Machines -> 0..MaxCommits]

\* fleet-claims: each claim on a change, first or a take-over, goes to exactly
\* one machine.
OneHolderPerEpoch ==
  \A g, h \in grants : (g[1] = h[1] /\ g[2] = h[2]) => g[3] = h[3]

\* fleet-claims: only the machine holding the claim ever moves the change's
\* branch, also after a take-over.
FencedPush == ~stalePush

\* fleet-claims: only the machine holding the claim ever has a merge accepted.
FencedMerge == ~staleMerge

\* fleet-claims: the fleet ref never records more active or parked claims than
\* the limit, also when a ready run starts its next scope.
WithinFleetLimit == Count(claim) <= MaxRuns
=============================================================================
