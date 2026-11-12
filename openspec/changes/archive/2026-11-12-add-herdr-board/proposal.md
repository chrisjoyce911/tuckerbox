# Proposal

## Why

`tuckerbox status` lists runs, but it doesn't show the shape of the work:
which proposals are waiting, which step each change is in, what is blocked
and on what, or how a change got where it is. Kelpie's board shows this
well. Its columns of cards each open into a description, dependencies, runs
and a timeline of hand-offs, and you can move work forward from there.

We want the same view for OpenSpec changes, inside Herdr, without the
board becoming a second place where work is stored. This is Tuckerbox's
first Herdr integration.

The owner also wants to review a change's spec and tasks from the board,
fix them there, and approve a change before any agent may start it. Today
nothing stops `tuckerbox run` from starting a proposal nobody has read.

## What Changes

- **Run history:** every run keeps an append-only event history: started,
  step started and finished, hand-offs with their summaries, rejections with
  findings, blocked, resumed, stopped, ready. `tuckerbox history <change>
  [--json]` shows it.
- **Change dependencies:** a change can declare the changes it depends on.
  A run waits as `queued (waiting on …)` until they are ready or archived.
- **Change edits:** `tuckerbox edit <change> <file>` opens the proposal,
  design, model, tasks or a spec delta in the person's editor, then
  validates the change strictly. `tuckerbox tasks <change> tick|untick
  <n.m>` changes one task's checkbox. `tuckerbox validate <change>` runs
  strict validation. All of them work in the change's worktree if it has
  one, else in the repository checkout, and refuse while a run is queued or
  running on the change.
- **Change approval:** `tuckerbox approve <change>` and `tuckerbox
  unapprove <change>`. Approval is a hash of the change's text, kept in the
  committed `tuckerbox.toml` with who approved and when. Ticking tasks keeps
  it. Any other edit makes it stale, on every machine, with no command
  needed. With `require_approval` (repository config, default `true`),
  `tuckerbox run` refuses a change that isn't approved or whose approval is
  stale.
- **Board data:** `tuckerbox board --json` returns one repository's board:
  - every active change, with its title and task progress from tasks.md
  - its approval state, dependencies, run state, current step and round,
    and history
  - recently archived changes

  The columns are derived from OpenSpec and run state, not stored anywhere.
- **Herdr board mod:** a Herdr app mod, shipped in this repository and
  installed with `tuckerbox herdr install`.
  - **Board:** Compact columns by default: Proposed, Approved when required,
    In progress (step/queued badges), Ready and Done. `[board] columns = "steps"`
    restores Queued and a column per workflow step. Each card shows its task progress, approval
    (approved, stale or none), agent, machine and state, and highlights
    changes that need you.
  - **Card detail:** the proposal, the task list with tick boxes, approval,
    dependencies, rounds with findings, and the history.
  - **Actions:** approve and unapprove, edit a file in your editor in a
    Herdr pane, tick and untick tasks, start (only once approved), stop,
    resume, re-run a step, follow a step's log in a Herdr tab, open the
    proposal beside the board, and open the change's worktree as a Herdr
    space.
  - **Machines:** the board gathers the same repository's runs from every
    connected Herdr machine.
- The board never stores task state, never writes a file itself, and never
  moves a card by itself. Every action runs a `tuckerbox` command, or opens
  the person's editor through one. Cards move when OpenSpec, approval or
  run state changes.

## Capabilities

### New Capabilities

- `run-history`: the event history every run keeps, and how it is read.
- `change-dependencies`: how a change declares dependencies and how runs
  wait for them.
- `change-edits`: editing a change's files, ticking tasks and strict
  validation through Tuckerbox, and keeping edits out of a running change.
- `change-approval`: approving a change, how approval lapses, and the
  `require_approval` gate on `tuckerbox run`.
- `board-data`: the machine-readable board for one repository: columns,
  cards and card detail.
- `herdr-board`: the Herdr app mod: what it shows, what its actions do,
  and how it combines machines.

### Modified Capabilities

`repo-config` adds `board.columns` with the existing machine-default and
repository-override precedence. The capabilities from `add-core-runner` aren't archived yet. This
change depends on it and adds new capabilities beside them. Two of them
add to `add-core-runner` behaviour: the run gate adds to `change-runs`, and
`require_approval` and `editor` are new config keys (see design.md).

## Impact

- Depends on `add-core-runner` (runs, status, the per-worktree state
  directory).
- Adds a JavaScript Herdr mod under `herdr/mods/tuckerbox-board/`. It runs
  only inside the Herdr app. Without Herdr, Tuckerbox is unaffected.
- The mod runs `tuckerbox` on the connected machines through Herdr's `exec`,
  so `tuckerbox` must be on the PATH there.
- `require_approval` defaults to `true`, so after this change a repository
  must approve each change before `tuckerbox run` starts it, or set
  `require_approval = false`.
- `tuckerbox approve`, `unapprove`, `edit` and `tasks` change files but never
  commit. The person commits and pushes, as with any edit.
