# Design

## Context

- `tuckerbox run` starts each change as a background process. That process
  holds the change lock, waits for dependencies and a slot, and runs the
  steps. Runs already outlive the command that started them.
- `run.json` is the current state of a change's run, and `history.jsonl` is
  its timeline. A run whose process died shows as stopped with the reason
  `runs.Crashed` (the lock is free while the state says queued or running).
- `add-herdr-board`: the run gate, dependencies, the board and its "needs
  you" count.
- `add-task-scope`: `--tasks next`; a scoped run is ready only when its
  scope is ticked; `complete` recorded with `ready`; `(ship)` tasks;
  partial runs don't ship; incomplete whole-change runs block in `mr` and
  `merge` mode (the last three from `fix-task-scope`).
- `add-priority`: the admission order.
- `add-operating-modes`: `mode` (`solo`, `fleet`, `team`) and
  `tuckerbox dispatch`.
- `add-forge-shipping` (proposed): `mr` and `merge` modes, archive in the
  merge request, the ship claim (`formal/ship.tla`).
- The rules: config in the repository, grants and tokens on the machine,
  one run per change, cross-platform Go, no daemon.

## Goals / Non-Goals

**Goals:**
- Approved changes go from Approved to merged with no further command.
- Approval stays the only human gate. Everything a person decided (stop,
  block, unapprove, edit) is left alone.
- Nothing new to keep in step: autopilot's decisions come from files that
  already exist, so it can be killed and started again at any time.

**Non-Goals:**
- **A system service.** No launchd, systemd or Windows service. The owner
  runs `tuckerbox autopilot` in a terminal or a Herdr pane, and stops it
  with Ctrl-C.
- **Placement across machines.** In `solo` mode autopilot looks after one
  machine. In `fleet` mode each pass calls `tuckerbox dispatch`
  (`add-operating-modes`), which owns placement and the fleet claim.
  `team` mode, with several people, is refused for now.
- **Fixing blocked runs.** A blocked review, a failed step or a lost ship
  claim waits for a person. Autopilot doesn't retry them.
- **Preemption.** A higher-priority change waits for a free slot.
- **Cleaning up worktrees** after a merge.

## Decisions

### A command of its own, not `run --watch`
`tuckerbox run` means "these changes, now". Its arguments (`<change>`,
`--tasks`, `--all`) don't fit "whatever is approved, from now on". A
separate command gets its own lock, its own `--dry-run`, and a clear name
in a Herdr pane or `ps`. `run --watch` was the alternative. It would make
`run`'s flags mean different things depending on one switch.

### The loop
```
lock autopilot (one per repository and machine) or refuse
loop:
  every 5 s: read this repository's run states (local files only)
  when a run ended, or every interval (60 s):
    git fetch the base branch (warn and go on if offline)
    for each active change, in admission order:
      decide (below)
    report runs that newly need a person
  on Ctrl-C: release the lock, exit; runs keep going
```

A change is started when:
- it is approved (the run gate, judged as `tuckerbox run` judges it)
- its dependencies are met (the engine's own rule, read-only)
- it has no run, or a ready run whose change has unticked tasks
- the repository's queued and running runs on this machine are fewer than
  `max_parallel`

Crashed runs are resumed (once, see below). Everything else is left alone:
active, blocked, failed, stopped by a person, ready with nothing left, or
shipping.

Starting only while a slot looks free matters. A run started early would
hold its change lock while it waits, and the owner couldn't edit or
unapprove that change meanwhile. If a person starts runs by hand at the
same moment, autopilot can start one too many. That run then queues, and
the slot locks still bound how many run (`formal/runs.tla`, `WithinLimit`).

### Starting and resuming are conditional
A pass reads a run's state, decides, and then starts or resumes it. A
person or the run itself can act in between:
- the owner unapproves the change
- a person starts the change by hand, it blocks, and autopilot's start
  would then restart a blocked run
- a person resumes a crashed run and stops it, and autopilot's resume would
  then resume a run the person stopped
- a person resumes a crashed run, it crashes again, and autopilot's resume
  belongs to the earlier crash

So every request autopilot makes carries the state and `since` time it saw
in `run.json`. Once the engine holds the change lock, it goes ahead only if
both still match and the approval rule holds. Otherwise it changes nothing,
and the next pass looks again.

**The approval rule:**
- a start (or next group) needs `approved`: the run gate, as for
  `tuckerbox run`
- a crash resume needs `approved` or `stale`, not `none`

A crash resume follows `tuckerbox resume`, which isn't gated
(`add-herdr-board`): a run's own plan or build step may have made the
approval stale, and that must not strand every crashed run. But `none`
means a person withdrew approval while the run was down, and autopilot
must not override that.

`formal/autopilot.tla` checks the design, with one broken variant per
check: no state check, state without `since`, approval judged on what was
seen instead of under the lock, and a crash budget counted per round.

### Group by group
With `scope = "group"`, autopilot always starts with `--tasks next`.
`add-task-scope` resolves `next` under the gate, to the first group with an
unticked task. When that run is ready and tasks are left, the next pass
starts it again with `--tasks next`. Each group is built and reviewed on
its own, on one branch. A one-group change behaves like a whole change.

`scope = "change"` keeps whole-change runs for repositories whose changes
are small.

### Partial and incomplete runs
`add-task-scope` (with `fix-task-scope`) owns when a change is complete and
what happens if it isn't: a partial scoped run is `ready` and doesn't ship;
an unscoped run that isn't complete is `blocked` in `mr` and `merge` mode;
`(ship)` tasks such as "Archive this change" don't count and are ticked in
the archive commit. Autopilot relies on those rules and adds none of its
own. So with `scope = "change"`, an incomplete change becomes `blocked` in
`mr` and `merge` mode, autopilot reports it, and no pass restarts it.

In `local` mode an incomplete whole-change run stays `ready` (today's
rule), and starting it again with no scope is refused, since it is the same
scope. So autopilot never asks for that start:
- in group mode it continues the run with `--tasks next`, a new scope
- in change mode it reports once that a person must finish or re-run the
  change, and the card needs you

It reads `complete` from `run.json`, so this costs no extra reads.

### Shipping and the end of a change
Autopilot doesn't drive shipping. The last group's run ships by itself:
archive, push, merge request, CI and review fixes, and in `merge` mode the
merge. Once merged, the archive is on the base branch. The next pass's
fetch sees the change gone from the active list, and dependents become
startable (`Dependencies when shipping`: only archived on the base counts).

This repository ships with `ship = "merge"`. `AGENTS.md` says a pull
request merges when CI is green and an independent review by a different
agent model has no open findings, and that approval is the owner's only
routine gate. Forge shipping merges when the forge reports the merge
request mergeable, so the cross-model review must be a forge rule: branch
protection that requires an approving review, which the reviewing agent's
account gives. Until that rule exists, the repository uses `ship = "mr"`
and the coordinating agent merges. With `ship = "mr"`, autopilot fills the
review queue; with `merge`, the main branch.

### Crashed runs
A run whose process died (reboot, kill) is the one case autopilot resumes.
The engine raises the round on every resume, so a stale result is never
accepted. So the budget can't be counted per step and round: each resume
would start a fresh count, and a run that kills its machine would loop.

The budget is a **crash streak**, read from history:
- autopilot's `resumed` events carry `by: autopilot`
- the streak is the number of those since the run's last `step_finished`,
  or its last `started`, `resumed`, `rerun` or `scope_started` by a person
- autopilot resumes a crashed run only while the streak is 0, so once per
  streak

Progress (a step reporting a result) or a person's action starts a new
streak. Since the count is in history, an autopilot restart doesn't reset
it.

### Restarts
Autopilot holds:
- `<common-dir>/tuckerbox/autopilot.lock`, an OS file lock, which the OS
  releases if autopilot dies
- `<common-dir>/tuckerbox/autopilot.json` (process id, machine, start
  time), which counts only while the lock is held, like `edit.json`

Nothing else. What to start, what is waiting, and how many crashes there
were all come from OpenSpec, `run.json` and history. Runs are separate
processes, so they finish while autopilot is down. A new autopilot carries
on from whatever state they reached. The "already reported" set is kept in
memory only, so a restart reports each run that needs a person once more.

### Asking for a person
- **Terminal:** one line per run that newly needs a person.
- **Board:** `needs_you` with a reason on cards that are blocked or failed,
  or ready with tasks left and a stale approval. The header counts them,
  as now, and shows whether autopilot is on.
- **Toast:** the board mod (or its drawer, which reads every 30 s) compares
  each read with the last one, and calls Herdr's `notify` for each card
  that newly needs you. The CLI can't show Herdr toasts, and mods can, so
  the toast is the mod's job. A board opened later shows the count, not a
  burst of toasts.

### Approval
Autopilot refuses to run with `require_approval = false`. Without the gate,
it would start every proposal as soon as it is merged, and the one human
gate would be gone. Next groups go through the gate again, so a builder
that changed the design in group 1 sends the change back to the owner.

### Settings
`[autopilot] scope` and `interval` may be set in machine or repository
config, the repository winning. Neither grants anything. `require_approval`
stays repository-only, as before.

## Risks / Trade-offs

- **Two autopilots on two machines in solo mode** would both start the
  same change. The ship claim (`formal/ship.tla`) lets only one ship, and
  the other blocks with `claimed by …`. Correct, but the agent time is
  wasted. Use `fleet` mode for several machines: its claim is taken before
  any work.
- **A tight loop of failures** costs agent time. Each failure stops that
  change until a person acts, so the cost is one run per change.
- **Polling the forge and git.** One `git fetch` per pass (60 s). Shipping
  runs poll the forge themselves, with backoff.
- **Approval is the only human gate.** With `merge` mode nothing else
  stops a change, so approval has to be read with care. Group-by-group
  runs keep each agent review small.
