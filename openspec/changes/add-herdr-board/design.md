# Design

## Context

`add-core-runner` gives runs, per-worktree state in the git dir, a machine index, and `status --json`. This change builds on those.

Kelpie (mjanjic01/herdr-kelpie) is the reference for the look:
- Columns of cards.
- A header with "working" and "needs you" counts.
- Cards with an owner, a run count and a Start or Unblock button.
- A detail view with description, pull requests, dependencies, runs and history.

Kelpie keeps tasks in its own store in `~/.config/herdr-app/board`, per machine. That's the part we don't copy.

A Herdr app mod is JavaScript (gpui-kit) and can:
- run programs on any connected machine (`exec(cmd, args, {on, cwd})`)
- read files there (`readTextFile(path, {on})`)
- open panes, tabs, spaces, dialogs and windows through `run(...)` and `openOverlay`
- render markdown (`TextView.markdown`)

It can't use Node, a DOM, or timers other than `cx.timer`.

## Goals / Non-Goals

**Goals:**
- A board that reads like Kelpie's, for OpenSpec changes.
- No board-owned state: OpenSpec plus run state is the only truth, so the board can't disagree with the CLI.
- The board works the same whatever machine it's opened on, and shows runs on every connected machine.
- The JSON is complete enough that a terminal board or another UI can be built on it later without changing Tuckerbox.

**Non-Goals:**
- **A terminal (TUI) board in `tuckerbox` itself:** possible later on the same JSON.
- **Agents as live Herdr tabs:** a later change. Until then, "Follow log" opens a tab that tails the agent's log.
- **MR/PR and CI on cards:** they arrive with `add-forge-shipping`, which adds fields to the same document.
- **Moving cards by dragging:** columns are derived. You move work with actions, not by placing cards.
- **Editing proposals or tasks in the board:** use your editor. The board opens the proposal beside it.

## Decisions

### Derived columns, no board store
Columns come from `openspec list --json`, each change's `tasks.md`, run state, and the archive folder.

Kelpie's own columns (Backlog, To do, In progress, Review, Done) are kept by hand. We replace them with:
- **Proposed** and **Queued**
- one column per workflow step, so a repo with plan/build/review gets those three
- **Ready** and **Done**

Blocked and failed cards stay in their step's column with a red flag and a "needs you" count in the header, as Kelpie does. So you can see *where* something is stuck.

Alternatives considered:
- **A stored kanban that agents update:** rejected, because it drifts from the repo.
- **Fixed columns:** rejected, because each repo defines its own steps.

### History as an append-only JSONL file
`<git-dir>/tuckerbox/history.jsonl` sits next to `run.json`. Only the run's own process appends to it, and it writes one line per event. JSONL survives crashes mid-write: a torn last line is skipped on read. `run.json` stays the current-state snapshot. History is the timeline.

### Dependencies in a sidecar inside the change folder
OpenSpec has no structured dependency field. `ship-all` works dependencies out by reading the proposals. Writing extra keys into `.openspec.yaml` risks OpenSpec rejecting them in a future version.

Instead, `openspec/changes/<id>/tuckerbox.toml` holds `depends_on`. It travels with the change and is archived with it. If OpenSpec later adds a dependency field, Tuckerbox reads that too, and the sidecar becomes the fallback.

Waiting for dependencies happens *before* taking a parallel slot, so a blocked chain can't use up the slots.

### The mod calls the CLI, never the files
The mod gets everything through `tuckerbox board --json [--change <id>]` run with `exec` on each machine. It does not read state files directly. The CLI owns the format, and the mod survives changes to the state layout.

Polling:
- **Header counts:** every 5 s while shown.
- **Card detail:** fetched when a card is opened.
- **Hidden copies:** slow down to 30 s, because Herdr keeps hidden mod copies running.

### Matching a repository across machines
Repositories are matched by normalised remote URL: scheme, `.git` and the `user@` part are removed, and SSH host aliases are resolved through `ssh -G`. Each machine's `tuckerbox` finds its checkout of that remote through its machine index. A repository with no remote is shown only on the machine it lives on.

### Actions
| Action | Command or Herdr call |
|---|---|
| Start | `tuckerbox run <id>` |
| Stop | `tuckerbox stop <id>` |
| Resume | `tuckerbox resume <id>` |
| Re-run step | `tuckerbox rerun <id> <step>` |
| Follow log | `run("tab.new", …)` with `tuckerbox logs <id> <step> --follow`, on the card's machine |
| Open proposal | `pane.split` with a markdown viewer |
| Open worktree | Herdr's built-in `worktree` mod with `action: "open"` |

### Where the mod lives
The mod lives at `herdr/mods/tuckerbox-board/` in this repo, and its version matches the binary. `tuckerbox herdr install` copies it into `~/.config/herdr-app/mods/` and runs `herdr-app check`. `tuckerbox doctor` warns when the installed mod is older than the binary.

## Risks / Trade-offs

- **Herdr's mod API is young and changes often.** The mod is kept thin (render plus `exec`) so breakage stays small. `herdr-app check` runs in `tuckerbox herdr install` and in CI when available.
- **Polling cost:** several machines × every 5 s × `tuckerbox board --json` means some process starts. Each call reads small files and should stay under 100 ms. To be measured, with the interval made configurable if needed.
- **Derived columns can surprise.** A change jumps from Build back to... Build in round 2, which doesn't look like movement. The card shows the round, and the history shows the loop.
- **The sidecar dependency file is Tuckerbox-specific.** It's harmless to OpenSpec, but people must know it exists. `tuckerbox check` and the board both show dependencies.
