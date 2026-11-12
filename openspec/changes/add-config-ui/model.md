# Model

## Model

Several writers can save the same config file: two Settings views (a board window and a docked pane, or two people on one machine), and a terminal running `tuckerbox config set`. Each reads the file, the person thinks, and then it saves. That is two or more writers updating shared state, so it is modelled. The model checks the compare-and-swap (`--if-match` under the file's lock) and that the check runs on what is written.

A pull request save is the same pattern against the base branch's file: it reads the revision there, and under the lock re-fetches and compares before it pushes. Merging two config pull requests is the forge's and the reviewers' business, not modelled (see design.md, Risks).

### Files

| File | Role |
|---|---|
| `formal/configsave.tla`, `formal/configsave.cfg` | Model; TLC must pass |
| `formal/configsave_broken_blindwrite.tla`, `.cfg` | Broken variant: no `--if-match`, so a writer saves what it read plus its edit; TLC must reject (`NoLostSave`) |
| `formal/configsave_broken_checkbeforelock.tla`, `.cfg` | Broken variant: the edit is checked against the file as read, then merged onto the file as it is now without a second check; TLC must reject (`SavesAddNoProblem`) |

### Actors and shared state

- **Writers (`Read`, `Lock`, `Save`, `Refuse`):** each reads the file and its revision, takes the file's lock later, and under the lock either saves or refuses. A refused writer may reload and try again, up to `MaxTries` reads.
- **State:** `file` (the edits the file holds), `rev` (how many writes it has had), `lock` (who holds the file's lock), `seen` and `seenRev` (what each writer read), `saved` (edits reported as saved).
- **The check:** edits `W1` and `W2` are each fine alone but invalid together, like removing step `build` while another edit sets `on_reject = "build"`.
- **Not atomic:** reading and saving. Anything can happen between them.

### Invariants

| Invariant | Meaning | Named by requirement |
|---|---|---|
| `NoLostSave` | An edit reported as saved is still in the file | config-save: No lost saves |
| `SavesAddNoProblem` | No save leaves the file with a problem the check finds | config-save: Checked before saving |
| `TypeOK` | Variables keep their types | (type check) |

### Bounds

`Writers = {w1, w2, w3}`, `W1 = w1`, `W2 = w2`, `MaxTries = 2`.
- Two writers with clashing edits show the check-before-lock bug; a third with an edit that clashes with nothing shows that it still saves around them.
- Two reads each let a refused writer reload and save again.

TLC (tla2tools 1.7.4, `scripts/tla-check.sh`) explores 345 distinct states in under a second. Each broken variant is rejected within 64 states.
