---------------------------- MODULE configsave ----------------------------
\* Saving one config file (openspec change add-config-ui). Several writers
\* (two Settings views, a terminal running `tuckerbox config set`) each read
\* the file, prepare one edit, and save it. A save takes the file's lock,
\* compares the file's revision with the one the writer read, checks the
\* result, and only then writes. A refused writer may reload and try again.
\*
\* The file is the set of edits it holds; its revision counts the writes.
\* Edits W1 and W2 are each fine alone but invalid together, like removing a
\* step while another edit points `on_reject` at it.
EXTENDS Naturals

CONSTANTS Writers, W1, W2, MaxTries

None == "none"

\* A writer's edit is named after the writer.
Edit(w) == w

\* The check `tuckerbox check` runs, reduced to the one bad pair.
Valid(S) == ~({W1, W2} \subseteq S)

VARIABLES
  file,     \* the edits the file on disk holds
  rev,      \* the file's revision: how many writes it has had
  lock,     \* the writer holding the file's lock, or None
  pc,       \* each writer's step: "idle", "read", "locked" or "done"
  seen,     \* the file each writer read
  seenRev,  \* the revision each writer read (what --if-match carries)
  saved,    \* edits reported to their writer as saved
  tries     \* reads per writer (bounded for TLC)
vars == <<file, rev, lock, pc, seen, seenRev, saved, tries>>

TypeOK ==
  /\ file \subseteq Writers
  /\ rev \in Nat
  /\ lock \in Writers \cup {None}
  /\ pc \in [Writers -> {"idle", "read", "locked", "done"}]
  /\ seen \in [Writers -> SUBSET Writers]
  /\ seenRev \in [Writers -> Nat]
  /\ saved \subseteq Writers
  /\ tries \in [Writers -> 0..MaxTries]

Init ==
  /\ file = {}
  /\ rev = 0
  /\ lock = None
  /\ pc = [w \in Writers |-> "idle"]
  /\ seen = [w \in Writers |-> {}]
  /\ seenRev = [w \in Writers |-> 0]
  /\ saved = {}
  /\ tries = [w \in Writers |-> 0]

\* `config show --json`: read the file and its revision, then prepare the edit.
Read(w) ==
  /\ pc[w] = "idle"
  /\ tries[w] < MaxTries
  /\ seen' = [seen EXCEPT ![w] = file]
  /\ seenRev' = [seenRev EXCEPT ![w] = rev]
  /\ tries' = [tries EXCEPT ![w] = @ + 1]
  /\ pc' = [pc EXCEPT ![w] = "read"]
  /\ UNCHANGED <<file, rev, lock, saved>>

\* `config set --if-match`: take the file's lock. Time may pass between Read
\* and Lock, and other writers may save meanwhile.
Lock(w) ==
  /\ pc[w] = "read"
  /\ lock = None
  /\ lock' = w
  /\ pc' = [pc EXCEPT ![w] = "locked"]
  /\ UNCHANGED <<file, rev, seen, seenRev, saved, tries>>

\* Under the lock: the revision still matches and the result passes the
\* check, so write the file as read plus this edit (temp file and rename).
Save(w) ==
  /\ pc[w] = "locked"
  /\ rev = seenRev[w]
  /\ Valid(seen[w] \cup {Edit(w)})
  /\ file' = seen[w] \cup {Edit(w)}
  /\ rev' = rev + 1
  /\ saved' = saved \cup {Edit(w)}
  /\ lock' = None
  /\ pc' = [pc EXCEPT ![w] = "done"]
  /\ UNCHANGED <<seen, seenRev, tries>>

\* Under the lock: the file changed since it was read ("changed since read"),
\* or the edit would add a problem. Write nothing; the writer may reload.
Refuse(w) ==
  /\ pc[w] = "locked"
  /\ (rev # seenRev[w] \/ ~Valid(file \cup {Edit(w)}))
  /\ lock' = None
  /\ pc' = [pc EXCEPT ![w] = "idle"]
  /\ UNCHANGED <<file, rev, seen, seenRev, saved, tries>>

\* Every writer has saved, or has given up after MaxTries reads.
Finished ==
  /\ \A w \in Writers : pc[w] = "done" \/ (pc[w] = "idle" /\ tries[w] = MaxTries)
  /\ UNCHANGED vars

Next ==
  \/ \E w \in Writers : Read(w) \/ Lock(w) \/ Save(w) \/ Refuse(w)
  \/ Finished

Spec == Init /\ [][Next]_vars

\* A save reported as done is never overwritten by a later save.
NoLostSave == saved \subseteq file

\* No save leaves the file with a problem the check would find.
SavesAddNoProblem == Valid(file)
=============================================================================
