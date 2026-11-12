# Design

## Context

`runs.Engine.ship` checks the scope (`scope.Check`) and completeness
(`scope.Unticked`) against the worktree's `tasks.md` before calling
`ship.Engine.Run`, which archives (if `shipping.archive`), pushes, opens the
merge request and watches CI. `(ship)` tasks already sit outside both checks
and are ticked by shipping before archive.

## Goals / Non-Goals

**Goals:** a CI-only task no longer blocks shipping; a change archived by a
step ships; local mode is unchanged for CI tasks.

**Non-Goals:** moving the archive step after CI; deciding whether CI is
"really" green beyond what the watch loop already decides.

## Decisions

- **The rule for a CI task:** text starting with `(ci)` (any case), or text
  where `pass`, `passes` or `green` is followed, in the same sentence, by
  `on` or `in` and then the word `CI` (upper case). That catches the
  existing wording ("Go tests pass on Linux, macOS and Windows CI") without
  catching "Add a CI workflow that passes go vet". A `(ship)` task is never
  a CI task.
- **Where ticking happens:** in the watch loop, the first time CI is green
  for the head, before the mr-mode ready or merge-mode merge. If there are
  unticked CI tasks, they are ticked, committed (`Tick <change> task 6.2`,
  or `tasks 6.2, 6.3`), pushed under the same claim as a fix push, and the
  loop waits for CI on that commit. With none left it goes on as before, so
  a merge is still pinned to a green head.
- **Archive order:** shipping archives before the first push, so by the time
  CI is green the change is usually archived. Ticking edits whichever
  `tasks.md` the change has, active or archived (the same lookup the
  proposal and `(ship)` ticks use). The brief asked for ticking "before the
  archive step if archive runs"; since archive precedes CI here, the tick
  commit lands after it on the same branch, which keeps the merged result
  the same.
- **Archived on the branch:** only when the change's folder is gone and an
  archive folder `<YYYY-MM-DD>-<change>` exists in the worktree. Then the
  scope and completeness checks are skipped (the archive is the builder's
  claim the change is done, and review has passed) and the run ships or, in
  local mode, is ready and complete. `ship.Engine.Run` sees the same state
  and records the archived folder as the archive path without running
  `openspec archive`.

## Risks / Trade-offs

- The tick commit restarts CI on the merge request, adding one CI run per
  change with CI tasks. Ticking before the push would avoid that but would
  claim CI passed before it had.
- A builder could archive a change with work left. Review still has to pass
  before the gate, and the merge request's CI and review still apply.
