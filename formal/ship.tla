---------------------------- MODULE ship ----------------------------
\* Shipping one change from several machines (openspec change add-forge-shipping).
\* Each machine's run may reach the ship stage for the same change. The forge
\* holds the shared state: the change branch's tip and the merge request.
\* Two forge operations are atomic and are the only ones the design relies on:
\*   - a push with --force-with-lease, a compare-and-swap on the branch tip
\*   - opening a merge request, refused while one is open for the branch
\* A person may push to the branch at any time. CI reports on each commit.
EXTENDS Naturals

CONSTANTS Machines, MaxCommits

None == "none"
Person == "person"
Commits == 1..MaxCommits

VARIABLES
  pc,         \* each machine's ship stage
  lease,      \* the branch tip each machine expects (0: no branch)
  tip,        \* the remote branch tip (0: no branch)
  author,     \* who pushed each commit
  next,       \* the next unused commit id
  ci,         \* CI state of each commit
  mr,         \* the merge request: "none", "open" or "merged"
  owner,      \* the machine named in the merge request's claim marker
  checked,    \* the commit each machine last saw green
  merged,     \* the commit the forge merged (0: none)
  lost        \* a push replaced a commit someone else pushed
vars == <<pc, lease, tip, author, next, ci, mr, owner, checked, merged, lost>>

Init ==
  /\ pc = [m \in Machines |-> "idle"]
  /\ lease = [m \in Machines |-> 0]
  /\ tip = 0
  /\ author = [c \in Commits |-> None]
  /\ next = 1
  /\ ci = [c \in Commits |-> "pending"]
  /\ mr = "none"
  /\ owner = None
  /\ checked = [m \in Machines |-> 0]
  /\ merged = 0
  /\ lost = FALSE

\* Push a new commit with --force-with-lease=<branch>:<lease>. The forge moves
\* the tip only if it still equals the lease; otherwise the push is refused and
\* the run blocks. A machine pushes when it reaches the ship stage ("idle") and
\* after each fix round ("claimed").
Push(m) ==
  /\ pc[m] \in {"idle", "claimed"}
  /\ next <= MaxCommits
  /\ next' = next + 1
  /\ author' = [author EXCEPT ![next] = m]
  /\ IF tip = lease[m]
       THEN /\ lost' = (lost \/ (tip # 0 /\ author[tip] # m))
            /\ tip' = next
            /\ lease' = [lease EXCEPT ![m] = next]
            /\ pc' = [pc EXCEPT ![m] = IF pc[m] = "idle" THEN "pushed" ELSE "claimed"]
       ELSE /\ pc' = [pc EXCEPT ![m] = "blocked"]
            /\ UNCHANGED <<lost, tip, lease>>
  /\ UNCHANGED <<ci, mr, owner, checked, merged>>

\* Open the merge request with this machine's claim marker. The forge refuses
\* a second open request for the same branch; the run then reads the open one
\* and continues only if its marker names this machine.
Open(m) ==
  /\ pc[m] = "pushed"
  /\ IF mr = "none"
       THEN /\ mr' = "open"
            /\ owner' = m
            /\ pc' = [pc EXCEPT ![m] = "claimed"]
       ELSE /\ pc' = [pc EXCEPT ![m] = IF mr = "open" /\ owner = m THEN "claimed" ELSE "blocked"]
            /\ UNCHANGED <<mr, owner>>
  /\ UNCHANGED <<lease, tip, author, next, ci, checked, merged, lost>>

\* A person pushes to the branch (no lease: people are not bound by it).
PersonPush ==
  /\ next <= MaxCommits
  /\ next' = next + 1
  /\ author' = [author EXCEPT ![next] = Person]
  /\ tip' = next
  /\ UNCHANGED <<pc, lease, ci, mr, owner, checked, merged, lost>>

\* CI finishes on a commit. A result never changes once reported.
CI(c) ==
  /\ c < next
  /\ ci[c] = "pending"
  /\ \E r \in {"green", "red"} : ci' = [ci EXCEPT ![c] = r]
  /\ UNCHANGED <<pc, lease, tip, author, next, mr, owner, checked, merged, lost>>

\* The claimant reads CI for the head it last pushed and sees it green.
SeeGreen(m) ==
  /\ pc[m] = "claimed"
  /\ mr = "open"
  /\ ci[lease[m]] = "green"
  /\ checked' = [checked EXCEPT ![m] = lease[m]]
  /\ pc' = [pc EXCEPT ![m] = "merging"]
  /\ UNCHANGED <<lease, tip, author, next, ci, mr, owner, merged, lost>>

\* Merge, pinned to the commit seen green: the forge refuses if the head moved.
MergeGuard(m) == tip = checked[m]

Merge(m) ==
  /\ pc[m] = "merging"
  /\ IF MergeGuard(m)
       THEN /\ mr' = "merged"
            /\ merged' = tip
            /\ pc' = [pc EXCEPT ![m] = "done"]
       ELSE /\ pc' = [pc EXCEPT ![m] = "claimed"]
            /\ UNCHANGED <<mr, merged>>
  /\ UNCHANGED <<lease, tip, author, next, ci, owner, checked, lost>>

\* The commit bound is used up: the runs left waiting stay as they are.
Finished ==
  /\ next > MaxCommits
  /\ UNCHANGED vars

Next ==
  \/ \E m \in Machines : Push(m) \/ Open(m) \/ SeeGreen(m) \/ Merge(m)
  \/ PersonPush
  \/ \E c \in Commits : CI(c)
  \/ Finished

Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ pc \in [Machines -> {"idle", "pushed", "claimed", "merging", "done", "blocked"}]
  /\ lease \in [Machines -> 0..MaxCommits]
  /\ tip \in 0..MaxCommits
  /\ next \in 1..(MaxCommits + 1)
  /\ ci \in [Commits -> {"pending", "green", "red"}]
  /\ mr \in {"none", "open", "merged"}
  /\ owner \in Machines \cup {None}
  /\ merged \in 0..MaxCommits

Claims(m) == pc[m] \in {"claimed", "merging"}

\* ship-claim: at most one machine ships a change, and it is the one the merge
\* request's marker names.
OneClaim ==
  \A m \in Machines : Claims(m) => (owner = m /\ \A n \in Machines \ {m} : ~Claims(n))

\* ship-claim: a run's push never replaces a commit another machine or a
\* person pushed.
NoLostPush == ~lost

\* forge-shipping: the forge only merges a commit whose CI was green.
MergedGreen == merged # 0 => ci[merged] = "green"
=============================================================================
