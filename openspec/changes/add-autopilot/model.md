# Model

## Model

Autopilot adds an actor that reads a run's state and later acts on it.
Between the read and the act, a person and the run itself can change that
state. The slot and change locks (`formal/runs.tla`) still decide how many
runs run and that a change has one run. They don't decide whether autopilot
is still allowed to act. That is a new check-then-act race, so it is
modelled, with the approval rule and the crash budget.

### Files

| File | Role |
|---|---|
| `formal/autopilot.tla`, `formal/autopilot.cfg` | Model; TLC must pass |
| `formal/autopilot_broken_unchecked.tla`, `.cfg` | Broken variant: no check that the run is still as seen; TLC must reject |
| `formal/autopilot_broken_nosince.tla`, `.cfg` | Broken variant: the state is compared, `since` isn't; TLC must reject (`ActsOnSeenAttempt`) |
| `formal/autopilot_broken_ungated.tla`, `.cfg` | Broken variant: the approval rule is judged on what autopilot saw, not under the lock; TLC must reject (`ApprovedAtStart`) |
| `formal/autopilot_broken_perround.tla`, `.cfg` | Broken variant: the crash budget is counted per step and round, so every resume (a new round) has a fresh budget; TLC must reject (`CrashBudget`) |

### Actors and shared state

- **Autopilot:** `Look(c)` reads one change's run state, `since` (as
  `attempt`) and approval into `seen`. Later, `AutoStart` starts or
  advances the change, `AutoResume` resumes a crashed run, or `AutoSkip`
  does nothing. `Restart` forgets `seen`, as a killed and restarted
  autopilot does.
- **The run:** `Finish` (a group done: `ready`, or `done` after the last
  group), `Block`, `Crash`, and `EditText`, a step that makes the approval
  `stale` mid-run.
- **A person:** `Approve` (approve or unapprove, only while no run is
  active, since it takes the change lock), `PersonStart`, `PersonStop` and
  `PersonResume`.
- **Shared state:** `state` and `attempt` (`run.json`: every state change
  gets a new `since`), `approval` (`approved`, `stale` or `none`, from the
  files), `left` (unticked groups) and `streak` (autopilot resumes since
  the run's last progress or person action, from history).
- **Atomic:** each run engine request, under the change lock: the check
  against what was seen, the approval rule, the crash budget, and the
  state change.
- **Not atomic:** `Look`, then `AutoStart` or `AutoResume`. Any number of
  other actions can come in between.
- **Rounds** are not modelled. The engine raises the round on every
  resume. The budget is counted per streak, not per round, and the
  `perround` variant shows what counting per round would do.

### Invariants

| Invariant | Meaning | Named by requirement |
|---|---|---|
| `ApprovedAtStart` | Every start or next group autopilot makes is of an `approved` change with no run or a ready run, at that moment | autopilot: Acting on what it saw |
| `PersonStopsHold` | Autopilot resumes only crashed runs, never a stopped or blocked one, nor one whose approval a person withdrew | autopilot: Autopilot never overrides a person; Acting on what it saw |
| `ActsOnSeenAttempt` | Autopilot acts only on the run attempt (state and `since`) it read | autopilot: Acting on what it saw |
| `CrashBudget` | At most one autopilot resume per crash streak, whatever the rounds | autopilot: Crashed runs |
| `TypeOK` | Variables keep their types | (type check) |

### Bounds

`Changes = {c1, c2}`, `Groups = 2`, `MaxAutoResumes = 1`, `MaxAttempt = 4`
(a state constraint, `AttemptBound`, since `since` only grows).
- Two groups give a ready run with work left, which autopilot advances.
- Four attempts per change allow start, crash, a person's resume and a
  second crash, the longest race here.
- Two changes let autopilot look at one while the other moves.

TLC (tla2tools 1.7.4, `scripts/tla-check.sh`) explores 115 657 distinct
states in about a second. Each broken variant is rejected within 8 states:
- `unchecked`: autopilot sees a change with no run, a person starts it and
  it finishes a group, and autopilot starts it again on its old read.
  Checked against the other invariants alone, it also fails
  `ApprovedAtStart` (it starts a crashed run) and `PersonStopsHold` (it
  "resumes" a run that a person resumed meanwhile and that has since
  finished a group).
- `nosince`: a run crashes, autopilot looks, a person resumes it and it
  crashes again, and autopilot resumes the second crash on its read of
  the first.
- `ungated`: autopilot sees an approved change, the owner unapproves it,
  and autopilot starts it.
- `perround`: a run crashes, autopilot resumes it, it crashes again, and
  autopilot resumes it again.

**What `since` adds.** Checked without `ActsOnSeenAttempt`, the `nosince`
variant passes: comparing the state alone, with the approval rule and the
budget judged under the lock, already keeps `ApprovedAtStart`,
`PersonStopsHold` and `CrashBudget`. So `since` isn't needed for those
properties. It is kept because it is one more field in a request, it makes
"acting on what it saw" literally true, and it guards against state
changes the model leaves out.
