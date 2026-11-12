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
- The owner can review, fix and approve a change from the board, and no agent starts a change nobody approved.
- The board works the same whatever machine it's opened on, and shows runs on every connected machine.
- The JSON is complete enough that a terminal board or another UI can be built on it later without changing Tuckerbox.

**Non-Goals:**
- **A terminal (TUI) board in `tuckerbox` itself:** possible later on the same JSON.
- **Agents as live Herdr tabs:** a later change. Until then, "Follow log" opens a tab that tails the agent's log.
- **MR/PR and CI on cards:** they arrive with `add-forge-shipping`, which adds fields to the same document.
- **Moving cards by dragging:** columns are derived. You move work with actions, not by placing cards.
- **An editor inside the board:** the board opens *your* editor in a Herdr pane. It has no text editing of its own.
- **Committing for you:** approve, unapprove, edit and tick leave their changes uncommitted. The person commits and pushes.
- **Re-checking approval on resume or re-run:** the gate is on starting a change (see Risks).

## Decisions

### Derived columns, no board store
Columns come from `openspec list --json`, each change's `tasks.md` and `tuckerbox.toml`, run state, and the archive folder.

Kelpie's own columns (Backlog, To do, In progress, Review, Done) are kept by hand. We replace them with:
- **Proposed:** no approval, or a stale one
- **Approved:** approved, no run yet
- **Queued**
- one column per workflow step, so a repo with plan/build/review gets those three
- **Ready** and **Done**

The new column is called Approved, not Review, because many repos have a workflow step called review, and its column would have the same name.

With `require_approval = false` there is no Approved column, and Start is offered on Proposed cards.

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
| Start (Approved cards only, unless approval is off) | `tuckerbox run <id>` |
| Approve, Unapprove | `tuckerbox approve <id>`, `tuckerbox unapprove <id>` |
| Edit | `pane.split` running `tuckerbox edit <id> <file>`, then `tuckerbox validate <id> --json` when the pane closes |
| Tick, Untick | `tuckerbox tasks <id> tick\|untick <n.m>` |
| Stop | `tuckerbox stop <id>` |
| Resume | `tuckerbox resume <id>` |
| Re-run step | `tuckerbox rerun <id> <step>` |
| Follow log | `run("tab.new", …)` with `tuckerbox logs <id> <step> --follow`, on the card's machine |
| Open proposal | `pane.split` with the board mod's proposal view, read through `tuckerbox board --json --change` |
| Open worktree | `space.select` of the space already in the worktree, else `space.new` there on the card's machine (what the built-in `worktree` mod's Open does, without its picker) |

### Edits go through `tuckerbox edit`, not straight to the editor
The board could open `$EDITOR` in a pane itself. Instead the pane runs `tuckerbox edit <id> <file>`, which starts the editor as its child. That gives three things:
- **One place for the rules.** Choosing the folder, the editor and the refusals is in the CLI. Without Herdr, `tuckerbox edit` works the same.
- **Exclusion from runs.** `tuckerbox edit` holds the change's run lock (`<git-dir>/tuckerbox/run.lock`) while the editor is open. `tuckerbox tasks`, `approve` and `unapprove` hold it while they write. A run process holds the same lock while queued or running. So an edit and a run can never overlap, and the check is the lock itself, not "read state, then write". While an edit holds the lock it keeps a marker, `<git-dir>/tuckerbox/edit.json` (command and pid), so a run or resume that finds the lock taken can say the change is being edited. The marker counts only while the lock is held, and a run that takes the lock removes one left by an edit that died. Approve and unapprove take the lock through the same helper (`edits.WithChangeLock`).
- **Validation when the editor exits.** `tuckerbox edit` runs strict validation and prints it in the pane. The board then runs `tuckerbox validate --json` and shows the result on the card. The board keeps that result in memory only.

The editor comes from machine config (`editor = "..."`), else `$VISUAL`, else `$EDITOR`. It is a command, so like permission flags only the machine may set it. A GUI editor works if it waits for the file to close (`code --wait`).

Run state can also be stopped, blocked, failed or ready. Then no process holds the lock and edits are allowed. That's when a person most wants to fix a change.

### Where edits happen
In the change's worktree if one exists on the machine, else in the repository checkout. The worktree is where the run will read the files next, so an edit there is what the next resume sees. A change that never ran has no worktree, so the checkout is the only copy. There is no run lock to take in that case, because no run can be using the checkout: runs always work in their own worktree.

### Approval is a hash in the committed sidecar
`approve` writes an `[approval]` table to `openspec/changes/<id>/tuckerbox.toml`, the sidecar `change-dependencies` already uses:

```toml
depends_on = ["add-users"]

[approval]
hash = "sha256:…"
by = "Sam Example"      # git user.name
at = 2026-11-12T09:00:00+10:00
```

Why a hash, and why in the committed sidecar:
- **It lapses by itself.** Any edit to the hashed text changes the hash, so the approval is stale without anyone running a command, and no edit path can forget to clear it. A flag such as `approved = true` would have to be cleared by every editor, agent and `git pull`.
- **Every machine agrees.** The approval travels with the change through git, like `depends_on`. A machine judges it from the files it has. Run state in the git dir would be per machine and lost with the worktree.
- **It is reviewable.** Approving shows up in a commit and a pull request, next to the text it approves. It is archived with the change.
- **OpenSpec stays untouched.** As with dependencies, we don't add keys to `.openspec.yaml`.

The hash is SHA-256 over proposal.md, design.md, model.md, tasks.md and each `specs/**/spec.md`, in path order. Each file adds its path, then its length and content, so moving text between files changes the hash, and a missing file differs from an empty one. Before hashing:
- line endings become LF, so a Windows checkout with CRLF agrees with the others
- in tasks.md, every `- [x]` or `- [X]` checkbox is read as `- [ ]`, so ticking tasks keeps the approval while changing a task's wording voids it

`tuckerbox.toml` is not hashed, because it holds the hash. So a change to `depends_on` or `title` keeps the approval. Those don't change what the agents are asked to build.

`approve` validates strictly first, and refuses an invalid change. It rewrites only the `[approval]` table and keeps the other keys. It doesn't commit, and says so.

### The run gate judges what the run will use
`tuckerbox run` checks approval against the files the run is about to work from:
- with a worktree, the change's files there
- without one, the change's files at the commit the new worktree would be made from, read from git before any worktree is made: the change's own branch if it still exists (a worktree removed with its branch kept is remade from that branch), else the base branch after the usual fetch

So an approval only lets a run start once it is committed where the run reads. If the checkout has an uncommitted approval, the refusal says so. `--all` skips changes that fail the gate and lists them, so one unapproved proposal doesn't stop the rest.

That check in `tuckerbox run` is early, for a clear message before any process starts. It can't be the last word: between it and the run taking the change lock, `unapprove` or an edit may land in the worktree, and the base branch may move. So the run engine judges again:
- **Before making a worktree,** at a pinned commit. `tuckerbox run` passes the commit it checked to the engine, which makes the new worktree at exactly that commit. A retained change branch must still be at the commit that was checked, or the run refuses. A commit can't change, so there is nothing to race.
- **Once it holds the change lock,** in the worktree, before it writes any run state. `approve`, `unapprove`, `edit` and `tasks` take the same lock to write. So the files judged are the files the run starts with, and a refusal leaves the run as it was.

Resume and re-run are not gated (see Risks).

### `require_approval` is a repository key
It defaults to `true`, and only the repository config may set it. A machine that switched it off would quietly run unapproved changes that other machines refuse. Like the workflow, the gate is the same on every machine that checks out the repository.

### How this fits `add-core-runner`
`run`, `resume`, the run lock and config validation belong to `add-core-runner`, which isn't archived yet. So this change keeps its rules in its own capabilities and doesn't write deltas against unarchived specs:
- The `Run gate` requirement in `change-approval` adds to `change-runs` once both changes are archived. It narrows `Selecting changes`: a valid change must also pass the gate.
- `require_approval` (repo only) and `editor` (machine only) are new keys for `repo-config`'s merge rules and `tuckerbox check`.
- "A run or resume refuses while an edit holds the lock" adds a message to `change-runs`' existing "Change already running" refusal.

`add-core-runner` must be archived first, as already planned.

### Where the mod lives
The mod lives at `herdr/mods/tuckerbox-board/` in this repo, and its version matches the binary. `tuckerbox herdr install` copies it into `~/.config/herdr-app/mods/` and runs `herdr-app check`. `tuckerbox doctor` warns when the installed mod is not the one the binary ships (older, or changed by hand), by the digest `tuckerbox herdr install` records in the mod's `version.json` and by comparing each shipped file's bytes.

## Risks / Trade-offs

- **Approving on a stale checkout.** The person may approve text in a checkout that is behind the base branch, or approve without committing. The hash protects against both: the run gate judges the files the run will use, so a stale or uncommitted approval refuses to start and says why. The card shows `committed: false` beforehand, so the Approved column doesn't hide this.
- **Approval covers the start, not the whole run.** A plan step may rewrite the design, which makes the approval stale mid-run. Resume and re-run don't re-check it, or every plan step would block its own run. The card shows the stale approval, so a person can see the text moved on.
- **An editor that outlives `tuckerbox edit`.** If `tuckerbox edit` is killed but its editor keeps running, the OS releases the lock while the file is still open, and a later save could land during a run. The editor runs in the same process group as `tuckerbox edit`, so closing the pane ends both. A save after that is a plain file edit, which the run's own review step sees.
- **Editing the checkout while a run starts.** With no worktree, edits go to the checkout. If a run starts meanwhile, it works from the base branch, not the checkout, so it doesn't see them. That's expected (the run gate judged the base branch), and the card then points edits at the new worktree.
- **TOML rewriting.** Keeping comments while rewriting `tuckerbox.toml` is awkward with Go TOML libraries. `approve` and `unapprove` edit only the `[approval]` table's lines and leave the rest of the file as text. Tests cover comments and other keys.
- **Pane close detection.** If Herdr can't tell the mod when a pane closes, the board offers a Validate button instead and shows "not validated since edit" on the card.

- **Herdr's mod API is young and changes often.** The mod is kept thin (render plus `exec`) so breakage stays small. `herdr-app check` runs in `tuckerbox herdr install` and in CI when available.
- **Polling cost:** several machines × every 5 s × `tuckerbox board --json` means some process starts. Each call reads small files and should stay under 100 ms. To be measured, with the interval made configurable if needed.
- **Derived columns can surprise.** A change jumps from Build back to... Build in round 2, which doesn't look like movement. The card shows the round, and the history shows the loop.
- **The sidecar dependency file is Tuckerbox-specific.** It's harmless to OpenSpec, but people must know it exists. `tuckerbox check` and the board both show dependencies.
