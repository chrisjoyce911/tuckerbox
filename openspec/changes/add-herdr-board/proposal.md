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

## What Changes

- **Run history:** every run keeps an append-only event history: started,
  step started and finished, hand-offs with their summaries, rejections with
  findings, blocked, resumed, stopped, ready. `tuckerbox history <change>
  [--json]` shows it.
- **Change dependencies:** a change can declare the changes it depends on.
  A run waits as `queued (waiting on …)` until they are ready or archived.
- **Board data:** `tuckerbox board --json` returns one repository's board:
  - every active change, with its title and task progress from tasks.md
  - its dependencies, run state, current step and round, and history
  - recently archived changes

  The columns are derived from OpenSpec and run state, not stored anywhere.
- **Herdr board mod:** a Herdr app mod, shipped in this repository and
  installed with `tuckerbox herdr install`.
  - **Board:** a column per stage of the repo's workflow. Each card shows its
    task progress, agent, machine and state, and highlights changes that need
    you.
  - **Card detail:** the proposal, the task list, dependencies, rounds with
    findings, and the history.
  - **Actions:** start, stop, resume, re-run a step, follow a step's log in a
    Herdr tab, open the proposal beside the board, and open the change's
    worktree as a Herdr space.
  - **Machines:** the board gathers the same repository's runs from every
    connected Herdr machine.
- The board never stores task state and never moves a card by itself.
  Every action runs a `tuckerbox` command, and cards move when the run's
  state changes.

## Capabilities

### New Capabilities

- `run-history`: the event history every run keeps, and how it is read.
- `change-dependencies`: how a change declares dependencies and how runs
  wait for them.
- `board-data`: the machine-readable board for one repository: columns,
  cards and card detail.
- `herdr-board`: the Herdr app mod: what it shows, what its actions do,
  and how it combines machines.

### Modified Capabilities

None. The capabilities from `add-core-runner` aren't archived yet. This
change depends on it and adds new capabilities beside them.

## Impact

- Depends on `add-core-runner` (runs, status, the per-worktree state
  directory).
- Adds a JavaScript Herdr mod under `herdr/mods/tuckerbox-board/`. It runs
  only inside the Herdr app. Without Herdr, Tuckerbox is unaffected.
- The mod runs `tuckerbox` on the connected machines through Herdr's `exec`,
  so `tuckerbox` must be on the PATH there.
