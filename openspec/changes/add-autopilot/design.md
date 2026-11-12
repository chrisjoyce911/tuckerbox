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
- `add-task-scope` (merged as a proposal): `--tasks next`, and a scoped run
  is ready only when its scope is ticked.
- `add-priority` (proposed): the admission order.
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
- **Several machines.** Autopilot looks after the runs of one machine.
  Deciding which machine runs which change is `add-operating-modes`.
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
- the owner unapproves the change (already caught: the run gate is judged
  again under the change lock)
- a person starts the change by hand, it blocks, and autopilot's start
  would then restart a blocked run
- a person resumes a crashed run and stops it, and autopilot's resume would
  then resume a run the person stopped

So every request autopilot makes carries the state and `since` time it saw
in `run.json`. Once the engine holds the change lock, it goes ahead only if
both still match. Otherwise it changes nothing, and the next pass looks
again. `since` changes on every transition, so a run that left the seen
state and came back is also refused. `formal/autopilot.tla` checks this
design. The broken variant, which acts on the earlier read, is rejected.

### Group by group
With `scope = "group"`, autopilot always starts with `--tasks next`.
`add-task-scope` resolves `next` under the gate, to the first group with an
unticked task. When that run is ready and tasks are left, the next pass
starts it again with `--tasks next`. Each group is built and reviewed on
its own, on one branch. A one-group change behaves like a whole change.

`scope = "change"` keeps whole-change runs for repositories whose changes
are small.

### Partial runs don't ship
In `mr` or `merge` mode, `add-forge-shipping` ships a run when its last step
reports `done`, and the first thing shipping does is archive the change.
A run for group 1 of 3 must not do that. So the forge path checks tasks.md
first: if any task is unticked, the run becomes `ready` and doesn't ship.
This holds for every run, by hand or by autopilot.

The rule belongs in `forge-shipping`, which isn't archived. It stays in
`autopilot` until both are archived, then moves there. Whichever of
`add-forge-shipping` and this change is built second wires it in.

### Shipping and the end of a change
Autopilot doesn't drive shipping. The last group's run ships by itself:
archive, push, merge request, CI and review fixes, and in `merge` mode the
merge. Once merged, the archive is on the base branch. The next pass's
fetch sees the change gone from the active list, and dependents become
startable (`Dependencies when shipping`: only archived on the base counts).

With `ship = "mr"`, a change stops at a green merge request for people to
merge. That is this repository's rule (the owner merges), so here autopilot
fills the review queue rather than the main branch.

### Crashed runs
A run whose process died (reboot, kill) is the one case autopilot resumes.
It resumes it once per step and round. Its `resumed` event carries
`by: autopilot`. If history shows the same step and round crashed again
after such a resume, autopilot leaves the run and reports it. So a run that
crashes its machine every time doesn't loop.

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

- **Two autopilots on two machines** for one repository would both start
  the same change. The ship claim (`formal/ship.tla`) lets only one ship,
  and the other blocks with `claimed by …`. Correct, but the agent time is
  wasted. Until `add-operating-modes`, the owner runs one autopilot per
  repository.
- **A tight loop of failures** costs agent time. Each failure stops that
  change until a person acts, so the cost is one run per change.
- **Polling the forge and git.** One `git fetch` per pass (60 s). Shipping
  runs poll the forge themselves, with backoff.
- **The owner merges the queue faster than they read it.** Approval is the
  only gate, so it has to be read with care. Group-by-group runs keep each
  review small.
