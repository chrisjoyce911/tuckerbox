# Model

## Model

Autopilot adds an actor that reads a run's state and later acts on it.
Between the read and the act, a person and the run itself can change that
state. The slot and change locks (`formal/runs.tla`) still decide how many
runs run and that a change has one run. They don't decide whether autopilot
is still allowed to act. That is a new check-then-act race, so it is
modelled.

### Files

| File | Role |
|---|---|
| `formal/autopilot.tla`, `formal/autopilot.cfg` | Model; TLC must pass |
| `formal/autopilot_broken_unchecked.tla`, `.cfg` | Broken variant: the engine doesn't check that the run is still in the state autopilot saw. It acts on the earlier read, and only the run gate's approval check remains. TLC must reject |

### Actors and shared state

- **Autopilot:** `Look(c)` reads one change's run state and approval into
  `seen`. Later, `AutoStart` starts or advances the change, `AutoResume`
  resumes a crashed run, or `AutoSkip` does nothing. `Restart` forgets
  `seen`, as a killed and restarted autopilot does.
- **The run:** `Finish` (a group done: `ready`, or `done` after the last
  group), `Block`, `Crash`, and `EditText`, a step that makes the approval
  stale mid-run.
- **A person:** `Approve` (approve or unapprove, only while no run is
  active, since it takes the change lock), `PersonStart`, `PersonStop` and
  `PersonResume`.
- **Shared state:** `state` (`run.json`), `approved` (the files' approval
  state), `left` (unticked groups) and `resumes` (autopilot resumes of the
  current crash, from history).
- **Atomic:** each run engine request, under the change lock: the check
  that the run is still as seen, the run gate's approval check, and the
  state change.
- **Not atomic:** `Look`, then `AutoStart` or `AutoResume`. Any number of
  other actions can come in between.

### Invariants

| Invariant | Meaning | Named by requirement |
|---|---|---|
| `ApprovedAtStart` | Every start or next group autopilot makes is of an approved change with no run or a ready run, at the moment it starts | autopilot: Acting on what it saw |
| `PersonStopsHold` | Autopilot only resumes a crashed run, never a stopped or blocked one | autopilot: Autopilot never overrides a person |
| `TypeOK` | Variables keep their types | (type check) |

### Bounds

`Changes = {c1, c2}`, `Groups = 2`, `MaxAutoResumes = 1`.
- Two groups give a ready run with work left, which autopilot advances.
- One crash resume is the rule, and enough to show a resume race.
- Two changes let autopilot look at one while the other moves. One change
  already shows both races.

TLC (tla2tools 1.7.4, `scripts/tla-check.sh`) explores 53 908 distinct
states in about a second.
- `autopilot_broken_unchecked` fails `ApprovedAtStart` after 5 states:
  autopilot sees an approved change with no run, a person starts it, it
  blocks, and autopilot starts the blocked run.
- Checked against `PersonStopsHold` alone, it fails after 7 states: a run
  crashes, autopilot looks, a person resumes it and it finishes a group,
  and autopilot's resume starts the run again.
