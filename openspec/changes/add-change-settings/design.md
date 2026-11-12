# Design

## Context

See proposal.md for why. What exists:
- `add-core-runner` (archived): steps already have `agent`, `model`, `effort` and `permissions` in repository and machine config. Repository values win, except permission profiles, which only machine config defines.
- `add-herdr-board` (active): the change sidecar `openspec/changes/<id>/tuckerbox.toml` with `title`, `depends_on` and `[approval]`; the approval hash, which leaves the sidecar out; the change lock that `edit`, `tasks`, `approve` and runs share; `board --json` and the board mod.
- `add-forge-shipping` (active): fix steps (`fix_ci`) are steps in `[steps]` that are not in the workflow. A change may override them too.

## Goals / Non-Goals

**Goals:**
- A change can pick its own agent, model and effort per step, and a permission profile name, without editing repository config.
- The repository decides what may be picked. The machine decides what a profile grants. A change can never widen either.
- A person can see, on every card, what each step will run with and why.
- An approval still means "a person agreed to what runs, with which agent and which permissions".

**Non-Goals:**
- **Per-change workflows** (adding, removing or reordering steps, `on_reject`, `max_rounds`). That changes the shape of the work, and review loops are a repository rule. A later change could add it.
- **Per-change role prompts.** A change's design and tasks already steer the agents.
- **Per-step machines.** See "Machines are per change".
- **Placement.** `add-operating-modes` decides where a change runs. This change only stores the preference.

## Decisions

### Where the settings live

In the sidecar `add-herdr-board` already uses, so they travel with the change, are reviewed with it, and are archived with it:

```toml
title = "Login page"
depends_on = ["add-users"]
machines = ["studio", "mini"]

[steps.build]
agent = "codex"
model = "gpt-5.5"
effort = "high"

[steps.review]
agent = "claude"
permissions = "readonly"

[approval]
hash = "sha256:…"
settings_hash = "sha256:…"
by = "Sam Example"
at = 2026-11-12T09:00:00+10:00
```

The tables use the same names as repository config (`[steps.<id>]`), so a person who knows one knows the other. Only four keys are allowed in them. Anything else, such as `command`, `args` or `role`, is refused by `tuckerbox check`.

Alternatives considered:
- **Keys in `.openspec.yaml`:** rejected for the same reason as `depends_on`: OpenSpec owns that file.
- **A `[changes.<id>]` table in repository config:** rejected. It separates the setting from the change, and every change's settings would conflict in one file.

### The allow-list

Repository config:

```toml
[overrides]
agents = ["claude", "codex"]
models = { claude = ["opus", "sonnet"], codex = ["gpt-5.5"] }
efforts = ["medium", "high"]
permissions = ["edit", "readonly"]
```

- **Deny by default.** No table, or no key in it, means a change may not override that key. A repository opts in to each kind of choice. An open default would let any change pick any agent the machine knows.
- **Models are listed per agent,** because a model name only means something to one CLI.
- **Repository only.** If a machine could widen the list, a change would run on one machine and be refused on another. Same reasoning as `require_approval`.
- **Profile names, not grants.** `permissions` lists names a change may ask for. The machine's `[permissions.<name>]` still decides the flags and `allowed_remotes`. So the worst a change can do is ask for a profile the repository already allows and the machine already grants for this remote.

The allow-list is checked when the override is written (`tuckerbox set`), by `tuckerbox check`, and again when a run, resume or re-run starts. The last check matters because the repository can narrow the list after a change was set up.

### Effective settings

For each step and key, first set wins: change, repository step, machine step, agent default.

One exception: when a change overrides `agent`, a `model` or `effort` inherited from repository or machine config is dropped, and the new agent's default is used unless the change sets one too. A Claude model name passed to Codex would fail, or worse, be read as something else. Models and efforts are free-form strings that `tuckerbox check` can't verify for each CLI, so this rule is what keeps them apart.

`permissions` is not dropped on an agent change: the profile is per name, and the machine profile says which flags it grants each agent. A profile with no flags for the new agent is refused, as it is today for a repository step.

### Machines are per change

The request was per-step machines. We chose per change. A run, its worktree and its change lock live on one machine (`formal/runs.tla`, one run per change). Running the build on one machine and the review on another would mean moving a worktree in the middle of a run, which `add-operating-modes` doesn't do either. A list per change also says what people mean in practice: "this change needs the machine with the GPU" or "run this one on the laptop".

### `tuckerbox set`

```
tuckerbox set add-login step.build.agent=codex
tuckerbox set add-login step.review.effort=high
tuckerbox set add-login machines=studio,mini
tuckerbox set add-login --unset step.build.model
tuckerbox set add-login --json ...   # result for the board
```

It works like `approve`:
- In the change's worktree if there is one, else in the checkout (where edits happen).
- Holds the change lock while it writes, through the same helper (`edits.WithChangeLock`). So it can't land while a run is queued or running, and the check is the lock, not "read state, then write".
- Rewrites only the lines of the key it sets, keeping comments and other keys, as `approve` does for `[approval]`.
- Never commits. It says a change counts elsewhere only once committed and pushed, because runs read the committed base branch when there is no worktree.
- Validates the value against the allow-list first, and refuses with the allowed values.

One key per call keeps the command simple for the board to build and for people to type. Several calls are cheap.

### What voids approval

The approval says a person read the change and agreed to let agents build it. The question is which settings change what they agreed to.

- **`agent` voids it.** A different CLI is a different program, with different tools, sandbox rules and failure modes. Claude and Codex read briefs differently and act differently on the same permission profile. A person who approved "Claude builds this" did not approve "Codex builds this".
- **`permissions` voids it.** A profile decides what the agent may do without asking: edit files, run commands, use the network. Moving from `readonly` to `edit` is exactly the kind of change an approval is for.
- **`model` and `effort` keep it.** They change how well and how expensively the same agent does the same job, under the same permissions. The repository's allow-list already bounds them. If every model bump voided approval, people would re-approve without reading, and the approval would mean less. A worse model is caught by the review step, as a worse result is today.
- **`machines` keeps it.** Placement does not change what runs or what it may do: each machine's profile grants are its owner's decision, made once in machine config.

How: the approval hash stays as it is (it covers the change's text, not the sidecar). `approve` adds a second hash, `settings_hash`, over the sorted lines `step.<id>.agent=<value>` and `step.<id>.permissions=<value>` for every override in the sidecar. Only the change's own overrides are hashed, not the effective values. A repository's own config changes go through its own review, and hashing them would void every approval in the repository at once.

The approval is `stale` when either hash is off. A missing `settings_hash` reads as the hash of no overrides, so approvals made before this change stay valid, and an approved change that then gains an agent override goes stale.

Two hashes, not one, because they are checked at different times (next section).

### Resume and re-run

`add-herdr-board` decided that resume and re-run don't check approval, because a plan step may rewrite the design and would block its own run. That still holds for the text hash.

The settings hash is different. No step writes the sidecar's step tables: only `tuckerbox set` or a person does. And a stopped or blocked run takes its settings afresh at the next step. Without a check, a person could stop an approved run, switch the build to an unapproved agent, and resume. So `resume` and `rerun` check `settings_hash` when approval is required, and refuse with the override that changed.

### Recording what ran

The effective settings, and the source of each value, are written to `run.json` when a step starts, and to its `step_started` history event. The card shows a running step's recorded settings, not today's config, so it tells the truth about what is running. The profile's name is recorded, never its flags, because flags are machine detail.

### How this fits `add-herdr-board`

`change-approval`, `board-data`, `herdr-board` and the change lock are in `add-herdr-board`, which isn't archived. As `add-forge-shipping` does, this change writes no deltas against them:
- `Settings that void approval` and `Resume checks approved settings` narrow `change-approval`'s `Approval hash` and its "resume isn't gated" rule.
- `settings-board` adds to `board-data`'s card fields and `herdr-board`'s card detail.
- `tuckerbox set` joins the commands that take the change lock.

`add-herdr-board` must be archived first. Folding these into those specs is a tidy-up after both are archived.

### Model

No TLA+ model (model.md). `tuckerbox set` writes one file under the change lock that runs and other edits already take, and `formal/runs.tla` covers that lock. Runs read the settings once they hold the lock, as the run gate already does.

## Risks / Trade-offs

- **[Model names drift]** → An allowed model the CLI no longer knows fails at step start with the CLI's message. `tuckerbox check` can't know every CLI's models. The allow-list is short and in one file, so it is easy to fix.
- **[A cheap model approved under a strong one]** → Accepted (see "What voids approval"). The card shows the model, and the history records it per step.
- **[Overrides set but not committed]** → Like approvals, they don't count where the run reads until committed. The card's `committed: false` covers the sidecar, which already holds the approval.
- **[Override for a fix step]** → `fix_ci` is in `[steps]`, so a change may override it. Shipping fix steps start under the same allow-list and approval rules.
- **[Two people set different values on two machines]** → Each writes its own checkout. Git merges or conflicts on the sidecar as on any file. Approval is judged from what is committed.
- **[TOML rewriting]** → The same line-level rewriting `approve` uses, with tests for comments, other tables and an empty table left behind.

## Migration Plan

- No change for repositories without `[overrides]`: no change may override, and nothing else changes.
- Existing approvals stay `approved` (missing `settings_hash`).
- A repository opts in by adding `[overrides]`. This repository can do so once the change is built.
- Rollback: remove `[overrides]`. Changes with overrides are then refused by `check` and `run` until the overrides are removed with `tuckerbox set --unset`.
