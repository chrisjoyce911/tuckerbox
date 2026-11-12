---------------------------- MODULE ship ----------------------------
\* Shipping one change from several machines (openspec change add-forge-shipping).
\* Each machine's run may reach the ship stage for the same change. The forge
\* holds the shared state: the change branch's tip and the merge request.
\* Two forge operations are atomic and are the only ones the design relies on:
\*   - a push with --force-with-lease, a compare-and-swap on the branch tip
\*   - opening a merge request, refused while one is open for the branch
\* A person may push to the branch at any time. CI reports on each commit, and
\* a check can be re-run on the same commit at any time, so a result that was
\* green can become pending or red again. Checks are either required (the
\* forge's branch protection enforces them when it accepts a merge) or not.
\* A person may approve or withdraw approval at any time. Reading CI and the
\* approval, then merging, is not atomic: only the forge's own check at merge
\* time is, and it covers required checks and required approval only.
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
  req,        \* state of each commit's required checks
  opt,        \* state of each commit's other (not required) checks
  approved,   \* the merge request has the approval protection requires
  mr,         \* the merge request: "none", "open" or "merged"
  owner,      \* the machine named in the merge request's claim marker
  checked,    \* the commit each machine last saw green
  seen,       \* commits some run saw with every check green and approved
  merged,     \* the commit the forge merged (0: none)
  gated,      \* required checks were green and approval held when it merged
  lost        \* a push replaced a commit someone else pushed
vars == <<pc, lease, tip, author, next, req, opt, approved, mr, owner, checked,
          seen, merged, gated, lost>>

Init ==
  /\ pc = [m \in Machines |-> "idle"]
  /\ lease = [m \in Machines |-> 0]
  /\ tip = 0
  /\ author = [c \in Commits |-> None]
  /\ next = 1
  /\ req = [c \in Commits |-> "pending"]
  /\ opt = [c \in Commits |-> "pending"]
  /\ approved = FALSE
  /\ mr = "none"
  /\ owner = None
  /\ checked = [m \in Machines |-> 0]
  /\ seen = [c \in Commits |-> FALSE]
  /\ merged = 0
  /\ gated = FALSE
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
  /\ UNCHANGED <<req, opt, approved, mr, owner, checked, seen, merged, gated>>

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
  /\ UNCHANGED <<lease, tip, author, next, req, opt, approved, checked, seen, merged, gated, lost>>

\* A person pushes to the branch (no lease: people are not bound by it).
PersonPush ==
  /\ next <= MaxCommits
  /\ next' = next + 1
  /\ author' = [author EXCEPT ![next] = Person]
  /\ tip' = next
  /\ UNCHANGED <<pc, lease, req, opt, approved, mr, owner, checked, seen, merged, gated, lost>>

Others == <<pc, lease, tip, author, next, mr, owner, checked, seen, merged, gated, lost>>

\* CI finishes a required or other check on a commit.
ReqDone(c) ==
  /\ c < next
  /\ req[c] = "pending"
  /\ \E r \in {"green", "red"} : req' = [req EXCEPT ![c] = r]
  /\ UNCHANGED <<opt, approved>> /\ UNCHANGED Others

OptDone(c) ==
  /\ c < next
  /\ opt[c] = "pending"
  /\ \E r \in {"green", "red"} : opt' = [opt EXCEPT ![c] = r]
  /\ UNCHANGED <<req, approved>> /\ UNCHANGED Others

\* Someone re-runs a check on the same commit: its result is pending again.
ReqRerun(c) ==
  /\ c < next
  /\ req[c] # "pending"
  /\ req' = [req EXCEPT ![c] = "pending"]
  /\ UNCHANGED <<opt, approved>> /\ UNCHANGED Others

OptRerun(c) ==
  /\ c < next
  /\ opt[c] # "pending"
  /\ opt' = [opt EXCEPT ![c] = "pending"]
  /\ UNCHANGED <<req, approved>> /\ UNCHANGED Others

\* A reviewer approves, or withdraws an approval.
Approval ==
  /\ approved' = ~approved
  /\ UNCHANGED <<req, opt>> /\ UNCHANGED Others

\* The claimant reads CI and the forge's verdict for the head it last pushed
\* and sees every check green and the approval present.
SeeGreen(m) ==
  /\ pc[m] = "claimed"
  /\ mr = "open"
  /\ req[lease[m]] = "green"
  /\ opt[lease[m]] = "green"
  /\ approved
  /\ checked' = [checked EXCEPT ![m] = lease[m]]
  /\ seen' = [seen EXCEPT ![lease[m]] = TRUE]
  /\ pc' = [pc EXCEPT ![m] = "merging"]
  /\ UNCHANGED <<lease, tip, author, next, req, opt, approved, mr, owner, merged, gated, lost>>

\* Merge, pinned to the commit seen green: the forge refuses if the head moved.
MergeGuard(m) == tip = checked[m]

\* What branch protection requires at the moment it accepts a merge.
Protected == req[tip] = "green" /\ approved

\* The forge enforces protection on this merge: the token has no bypass.
ForgeGate == Protected

\* The forge checks the pin and protection atomically with the merge. If it
\* refuses, the run goes back to polling.
Merge(m) ==
  /\ pc[m] = "merging"
  /\ IF MergeGuard(m) /\ ForgeGate
       THEN /\ mr' = "merged"
            /\ merged' = tip
            /\ gated' = Protected
            /\ pc' = [pc EXCEPT ![m] = "done"]
       ELSE /\ pc' = [pc EXCEPT ![m] = "claimed"]
            /\ UNCHANGED <<mr, merged, gated>>
  /\ UNCHANGED <<lease, tip, author, next, req, opt, approved, owner, checked, seen, lost>>

\* The commit bound is used up: the runs left waiting stay as they are.
Finished ==
  /\ next > MaxCommits
  /\ UNCHANGED vars

Next ==
  \/ \E m \in Machines : Push(m) \/ Open(m) \/ SeeGreen(m) \/ Merge(m)
  \/ PersonPush
  \/ Approval
  \/ \E c \in Commits : ReqDone(c) \/ OptDone(c) \/ ReqRerun(c) \/ OptRerun(c)
  \/ Finished

Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ pc \in [Machines -> {"idle", "pushed", "claimed", "merging", "done", "blocked"}]
  /\ lease \in [Machines -> 0..MaxCommits]
  /\ tip \in 0..MaxCommits
  /\ next \in 1..(MaxCommits + 1)
  /\ req \in [Commits -> {"pending", "green", "red"}]
  /\ opt \in [Commits -> {"pending", "green", "red"}]
  /\ approved \in BOOLEAN
  /\ seen \in [Commits -> BOOLEAN]
  /\ gated \in BOOLEAN
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

\* forge-shipping: Tuckerbox only merges the commit it saw with every check
\* green and the approval present (checks may change after that read).
MergedGreen == merged # 0 => seen[merged]

\* forge-shipping: when the forge accepted the merge, the required checks on
\* the merged commit were green and the required approval held, even if a
\* check was re-run or an approval withdrawn after Tuckerbox's read.
MergedGated == merged # 0 => gated
=============================================================================
