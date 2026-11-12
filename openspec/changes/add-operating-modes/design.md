# Design

## Context

See proposal.md for why. What exists or is planned:
- `add-core-runner` (archived): one machine; the change lock and slots (`formal/runs.tla`); its plan named `add-remote-machines` (`machines = [...]`, SSH or `hc run --on`, `status --all-machines`). This change absorbs it.
- `add-herdr-board` (active): approval and the run gate, `board --json`, the board mod, which already merges cards from every connected Herdr machine by remote URL.
- `add-forge-shipping` (active): forge access and tokens, the machine name (`machine` in machine config), and the ship-time claim: a lease push of the change's branch plus "one open merge request per branch", with a marker naming change, machine and run (`formal/ship.tla`). It decided against a draft claim during the steps, and said one could be added later without changing the ship-time rules.
- `add-change-settings` (proposed): per-change `machines` preference and effective agents per step.

Rules this design keeps: config lives in the repository; grants and tokens stay on the machine; one run per change; cross-platform Go; no daemon.

## Goals / Non-Goals

**Goals:**
- An owner with several machines runs `tuckerbox dispatch` (or clicks Dispatch) and approved changes start on suitable machines.
- No two machines ever work on, or ship, the same change because of a race, a crash or a late wake-up.
- A team sees who holds each change, and only listed people's approval lets a run start.
- Solo repositories don't change.

**Non-Goals:**
- **A daemon or watcher.** Dispatch is one pass. People run it, click it, or schedule it with their own tools (cron, launchd, Task Scheduler, a herdr-core schedule).
- **Moving a run between machines.** A take-over starts again from the remote; the old machine's worktree stays as it was. Checkpointing work to the remote during the steps is a later change.
- **Automatic take-over.** A person decides (see Open Questions).
- **Fleets inside a team** (each person with several machines). One mode per repository for now.
- **Copying credentials between machines.** Each machine signs in its own agents.
- **Load balancing by CPU or memory.** Capacity is a number the owner sets.

## Decisions

### Configuration

Repository config, committed:

```toml
mode = "fleet"          # "solo" (default), "fleet" or "team"

[fleet]
max_runs = 4            # claims across all machines (none: no limit)
lease = "10m"           # an active claim not renewed for this long expires

[team]
approvers = ["sam", "alex"]   # forge logins whose approval counts
```

Machine config, never committed:

```toml
machine = "studio"      # from add-forge-shipping; default: the host name
fleet_capacity = 2      # dispatched runs this machine takes at once, all repositories (default 0)

[machines.mini]
ssh = "mini"            # an SSH destination, e.g. an alias in ~/.ssh/config

[machines.lab]
hc = "lab"              # a herdr-core machine, started with hc run --on
```

`add-core-runner` sketched `machines = [...]` in config. It is machine config here: how to reach a machine (host, user, key, alias) is that person's network detail, and a repository must not name hosts. The repository only says the mode and its limits.

`fleet_capacity` defaults to 0, so a machine takes fleet work only when its owner says so. A laptop listed for status doesn't silently become a worker.

### Why a git ref for fleet claims

Options for the shared state between an owner's machines:

| Option | Why not |
|---|---|
| The forge (a draft merge request) | Needs a token on every machine and forge API calls for every claim and renewal; doesn't work for `ship = "local"` or a remote without an API. |
| A file on a shared drive | Not every machine shares one; locks over network file systems are unreliable. |
| Herdr or herdr-core state | Tuckerbox works without Herdr. |
| A coordinator service | That's a daemon. |
| **A ref on the git remote** | Every fleet machine already fetches and pushes it with git's own credentials. `--force-with-lease` is a compare-and-swap, and `--atomic` covers several refs at once. |

So the claims are one ref, `refs/tuckerbox/fleet`, holding a commit whose tree has one small TOML file per claim (`claims/<change>.toml`: machine, run, epoch, state, renewed_at). One ref, not one per change, so the fleet limit is exact: a claim push checks the count in the same compare-and-swap. The cost is contention: every claim and renewal moves the same ref. With a handful of machines and renewals every two minutes that is a few pushes a minute; a refused push fetches and tries again.

The ref is outside `refs/heads/` and `refs/tags/`, so CI does not run on it, it doesn't show as a branch, and a normal `git fetch` doesn't fetch it. Tuckerbox fetches it by name. The commits are tiny and each has the previous as parent, so the history of claims can be read with `git log refs/tuckerbox/fleet` when debugging. Old history can be squashed by a release push later if it grows.

### Claim lifecycle

```
            run (claim push)          stop / block / fail
  (none) ────────────────► active ─────────────────────► parked
     ▲                      │  ▲  ◄──── resume / rerun ────┘
     │      merged / release │  │ renew (every lease/5)
     │                      ▼  │
     └────────────────── ready ◄─ run becomes ready
                     no renewal for `lease`
              active ─────────────────────► expired ──run --take-over──► active (epoch+1)
```

- **Active** claims are renewed by the run's own process, the background `tuckerbox run` that already exists for each run. That is not a daemon: it lives exactly as long as the run.
- **Parked** claims belong to stopped, blocked and failed runs, which have no process. They don't expire, because "blocked overnight" is normal, and a machine shouldn't lose its change because nobody looked. They still count toward `max_runs`.
- **Ready** claims keep other machines from starting a finished change. They don't count toward the limit. `merged` removes the claim; any fleet ref write also drops claims of changes now archived on the base.
- **Expired** is not stored. A reader works it out from `renewed_at` and `lease`, allowing 1 minute of clock skew. Nothing removes an expired claim by itself: a person takes over or releases it.

### Fencing: why clocks don't decide safety

A machine can be slow, asleep or cut off at any stage, including after it started shipping, and still believe it holds its claim. Leases can't stop that; fencing does. A shipping run has three kinds of shared effect, and each is fenced:

- **Every push of the change's branch**, the first and each fix round's, is one atomic push: `git push --atomic --force-with-lease=refs/heads/<b>:<lease> --force-with-lease=refs/tuckerbox/fleet:<seen> <remote> <b> <renewal>:refs/tuckerbox/fleet`. Both refs move, or neither. A renewal commit is pushed with it because git only checks a lease on a ref the push updates.
- **A merge** is a forge API call, which can't join a git push. It is fenced by the branch instead: the merge is pinned to the commit seen green (`add-forge-shipping`), and every take-over or release of a claim on a branch moves its tip (the next sections). So an old holder's pinned merge names a commit that is no longer the tip, and the forge refuses it. The run also renews just before merging, which closes most of the window early, but safety doesn't depend on that.
- **Replies on review threads and marker edits** only follow a push or a take-over by the same run, so an old holder whose push is refused makes none. A late reply or edit can't change what merges.

The steps themselves only change the machine's own worktree, so a zombie working on is wasted effort, not damage. So expiry timing affects only how soon a person may take over, never whether two machines can both push or merge. The model checks this by letting `Expire` happen at any moment, and by letting any machine pause between any two of its actions.

### Releasing a claim

`tuckerbox claim release <change>` (this machine's claim, no run active) and `--force` (another machine's) remove a claim by hand. Removing it from the fleet ref alone isn't enough: a holder that renewed just before its pinned merge, then paused, would wake and merge, because the branch tip is unchanged and the forge accepts the pin. So a release is built like a take-over. When the change's remote branch exists, one atomic push leases the fleet ref and removes the claim, and leases the branch at the fetched tip and moves it to an empty `Release <change> from <machine>` commit. The old holder's pinned merge and its next push then name an old tip and are refused; its next renewal finds no claim and blocks with `claim released`. Without a branch, only the fleet ref changes, and the old holder's first push is refused by its fleet ref lease. The same rule covers releasing this machine's own claim, so there is one release path to test. `formal/fleet_broken_releasekeep.tla` shows the forced release without the tip move letting the old holder merge.

### The next task scope

`add-task-scope` lets a ready (or stopped, blocked, failed) run start its next group with `tuckerbox run <change> --tasks <scope>`, in the same run, worktree and branch; `add-autopilot` relies on it. That run already holds a claim, so the next scope reuses it rather than claiming again.

**Fleet.** A ready claim doesn't count toward `max_runs`, which is the point: a change waiting between groups shouldn't block other changes. So other changes may take that room meanwhile, and starting the next scope is an admission, not a renewal:
- On the holder's machine, one lease push turns the claim from `ready` to `active`, same holder and epoch, and counts it against `max_runs` in the same compare-and-swap. If the limit is full, the push isn't made: `fleet limit reached`, naming the claims that fill it, and the run and claim stay ready. Nothing is lost; `run` again later, or autopilot's next pass, tries again.
- From another machine it is refused (`claimed by`): the worktree, with the earlier scopes' work, is on the holder's machine. Moving it needs `claim release` and a fresh start there.
- A parked claim (stopped, blocked or failed run) already counts, so it becomes active as on `resume`, with no new limit check.
- A dispatched run records `dispatched` in `run.json`. It freed its machine-wide fleet slot when it became ready, so its next scope takes one again first, before touching the claim; a full machine refuses with `<machine> is full` and the claim stays ready. Slot first, then claim, as at the start.

`formal/fleet.tla` models this: `BecomeReady` (the claim stops counting) and `NextScope` (back to active only if `ScopeAdmit`, the limit check, holds). `fleet_broken_scopecount` drops the check, and TLC finds the 7-state behaviour where `m1`'s change is ready, `m2` claims another into the freed room, and `m1`'s next scope goes over the limit.

**Team.** The claim is the branch and its draft merge request, which partial scopes don't touch (`add-task-scope`: partial runs don't push). The next scope checks the claim is still this run's (open draft with its marker and login, remote tip equal to the run's last push, so the start commit) and starts the scope with no new start commit and no push. A closed draft, a moved tip or another person's marker refuse with `ship-claim`'s reasons. The run gate applies as at any start: approval from the protected base, with the listed login. No new model: nothing on the forge changes.

### Claiming before the worktree

`tuckerbox run` in fleet mode: run gate (approval, settings), then fetch the fleet ref and claim, then the change lock and worktree as today. Claiming first means a refused run leaves nothing behind. The change lock (`runs.tla`) still guards runs on one machine; the claim guards between machines.

`resume` and `rerun` re-activate a parked claim with a lease push and refuse if the claim isn't theirs, so a run whose claim was taken over can't come back to life.

### Take-over

`tuckerbox run --take-over <change>` claims an expired claim with epoch + 1 (`--force` also for a parked one). The old machine's worktree is left alone: Tuckerbox never deletes work. When the old machine wakes, its next renewal, push or merge fails and its run blocks with `claim taken by <machine>`.

**No remote branch** (the old holder was still in its steps, since fleet runs push nothing before the ship stage): the claim is an ordinary fleet ref push, and the run starts at the first step from the base branch.

**A remote branch exists** (the old holder was shipping). One atomic push does three things: leases the fleet ref at the version fetched and writes the new claim; leases the branch at the tip fetched; and moves the branch to a new empty commit, `Take over <change> from <machine>`, on top of that tip. If anything moved since the fetch, nothing changes and the run fetches and decides again. After it:
- the old holder's branch lease no longer matches, so its fix pushes are refused
- its pinned merge names an old commit, so the forge refuses it
- the new holder's own lease is the take-over commit

Without moving the tip, the new holder would adopt the old holder's head, and an old holder that saw that head green could still merge it. `formal/fleet_broken_nomove.tla` shows TLC finding exactly that.

The take-over commit is not `[skip ci]`: CI runs on it, so `merge` mode has a green head of its own to pin.

**The forge claim.** `add-forge-shipping`'s `ship-claim` says a machine holds the forge claim when its own lease push last moved the branch and the open merge request's marker names it. After a take-over the first half holds (the take-over push), and the marker still names the old machine. So in fleet mode the marker carries `epoch=<n>`, and a run whose fleet claim has a higher epoch than the marker rewrites it to its own machine, run and epoch. Until then, other runs already defer to the fleet ref: in fleet mode the fleet claim, not the marker, decides who may ship. This narrows `ship-claim`'s `Losing the claim race`, `Resuming a claim` and `Claims end with the merge request`, and `forge-shipping`'s `Starting a shipping run` (a marker with a lower epoch than the fleet claim is not "another machine's claim"). If the process dies between the push and the rewrite, `resume` finds the tip is its own push and its epoch higher, and rewrites then.

**Where the run continues.** A remote branch means the old holder reached the ship stage, so the new worktree is made from the take-over commit and the run continues in the ship stage: no workflow step runs again. The branch already holds the archive commit, so the run doesn't archive again (`openspec/changes/<id>/` is gone and `archive/` has it). If no merge request is open (the old holder died between push and open), it opens one with its own marker.

End to end, when `mini` dies while CI runs on `add-login` and `studio` takes over:

```
mini:   push abc (archive + work)  → open MR (marker mini, epoch 1) → poll CI … (dies)
fleet:  claim add-login: mini/1, renewed 09:00 → expired 09:10
studio: run --take-over add-login
        atomic push: fleet ref (studio/2, lease v7) + branch (lease abc → def "Take over add-login from mini")
        rewrite marker → studio, epoch 2; poll CI on def; fix or merge pinned to def
mini:   wakes; push fix (lease abc) → refused; merge pinned abc → refused; renew → claim taken by studio → blocked
```

### Dispatch

```
tuckerbox dispatch [change...] [--dry-run] [--json]
  fetch base and fleet ref
  changes  = active, valid, run gate passes, dependencies met, no claim  (Approved column order)
  reports  = tuckerbox machine --json --for <remote> on this machine and each [machines.*], in parallel, 30 s timeout
  for each change, until max_runs:
    candidates = answered, free capacity, has checkout, every step's agent installed and not signed out
    pick       = first in change.machines that is a candidate; else most free capacity; else name
    start      = ssh <dest> -- tuckerbox run --for <remote> <change>   |  hc run --on <name> -- ...  | local
  print what happened, exit
```

- **Placement is advice.** Reports are a snapshot; another dispatcher, for this or another repository, may act at the same time. The started run decides: it takes a machine-wide fleet slot, then claims the change. A refusal (machine full, or claimed elsewhere) is reported, not retried elsewhere in the same pass, which keeps a pass simple and bounded.
- **Machine-wide admission.** `fleet_capacity` is a limit, not a hint. Dispatch starts runs with `--dispatched`; such a run takes one of `fleet_capacity` OS file locks, `$XDG_STATE_HOME/tuckerbox/fleet-slots/<n>.lock` (the machine state directory on each OS), before claiming. These are the same kind of slot locks `add-core-runner` uses per repository, in a machine-wide folder, so `formal/runs.tla`'s `WithinLimit` covers them with `MaxParallel = fleet_capacity` and processes from any repository as `Procs`. The slot is held by the run's process while it is queued, running or shipping, and freed when it ends, stops or blocks; `resume` of a dispatched run takes one again. A person's own `tuckerbox run` (no `--dispatched`) takes none. Taking the slot before the claim means a full machine never holds a claim it can't work on.
- **`--for <remote>`** finds the machine's checkout of the repository through the machine index the board already uses (normalised remote URL). A machine without a checkout is not a candidate; dispatch never clones.
- **Agents per change** come from `add-change-settings`' effective settings: a change whose build step uses Codex needs a machine with Codex.
- **Sign-in `unknown` is a candidate,** with a note. Refusing unknown would rule out every custom agent.
- **SSH** runs without a terminal (`ssh -T -o BatchMode=yes`), so it never prompts. `hc run --on` is used when the entry says `hc`; herdr-core then shows the run as a terminal on that machine.

Why not let each machine pull work itself (Muster's watcher)? That needs a process on each machine checking on a timer: a daemon. Dispatch from any one machine, on demand, gives the same result, and a person who wants it periodic can schedule `tuckerbox dispatch` with their own tools.

### Sign-in checks

`doctor` and `machine --json` ask each installed built-in agent CLI whether it is signed in, with its own non-interactive status command, a 5-second timeout, and without starting a session or spending tokens. Anything else is `unknown`. The exact commands are settled when implemented (Open Questions); the spec only needs the three states.

### Team mode: the forge claim, taken at the start

Team mode uses `add-forge-shipping`'s claim unchanged in its rules (lease push of the branch, one open merge request per branch, the marker), and takes it when the run starts:

1. Run gate (approval by a listed approver).
2. Make the branch with one empty commit, `Start <change>` with `[skip ci]` in its message.
3. Push it with the lease that expects no branch.
4. Open a **draft** merge request with the marker, now with `by=<forge login>`.
5. Run the steps on top of that commit.
6. At the ship stage, find that draft (its marker is ours), update its title and body, and mark it ready.

`add-forge-shipping` gave three reasons against a draft during the steps; each is met here:
- *GitHub refuses a pull request with no commits beyond the base:* the empty start commit is one.
- *Every push during the steps starts CI:* there is one push, marked `[skip ci]`, which GitHub Actions and GitLab both honour. Nothing else is pushed until the ship stage.
- *Step commits may be amended or rebased:* they build on top of the start commit, and the first push after it is at the ship stage, a fast-forward with the lease.

So `ship.tla`'s argument covers team mode as is: the start push is the model's first `Push`, and opening the draft is `Open`. `OneClaim` and `NoLostPush` hold. No new model is needed for team mode.

Why not the fleet ref for teams too? People work from their own machines and see their work on the forge. A draft merge request with an owner is visible to everyone, including people without Tuckerbox, and the forge already knows who can push. A fleet ref is invisible on the forge.

### Approvers

`tuckerbox approve` in team mode asks the forge who the token belongs to and records `login` in `[approval]`. That alone proves nothing: `tuckerbox.toml` is a text file, the hashes are public, and anyone can recompute them and type a listed login into a local branch or worktree.

`add-herdr-board`'s run gate reads the worktree, else a retained change branch, else the base. That is right for one trusted person, and wrong for a team. So in team mode the gate reads the approval **only from the base branch, at the commit just fetched from the remote**, and:
- the approval's `login` must be in `[team] approvers`
- the files the run will use (a new worktree made at that base commit, or an existing worktree) must match that approval's hashes; an existing worktree or retained branch whose files differ is refused, whatever its own `[approval]` says
- the forge must confirm the base branch refuses direct pushes and needs a merge request with an approving review; if it doesn't, or the token can't read the rules, the run refuses

The same holds for `resume` and `rerun`. They don't re-check the text hash (a plan step may rewrite the design), but they do check the settings hash (`add-change-settings`), and in team mode they take that `settings_hash` from the approval on the base just fetched, with a listed login, never from the worktree's own `[approval]`. Otherwise a stopped run's worktree could get an unapproved agent and a recomputed hash, and resume.

So an approval counts only if it reached the protected base through a reviewed merge request. Someone with push access can still put a listed login into an approval merge request, but a reviewer with rights on the base must approve it, and both are on the forge's record. That is the trust the repository already gives its base branch.

Alternatives considered:
- **An approving forge review as the approval itself** (for example, a review on the change's draft merge request from a listed login). It authenticates the person directly, but the draft is only opened at the start, after the gate; it would need a separate "approval request" step and per-forge review semantics. Kept as an open question.
- **Signed approval commits.** Needs every approver to set up signing, and the forge's "verified" flag to be read per commit. Same trust as protection, more setup.

### Board

`board --json` gains `mode`, the fleet count, and `claim` on cards. In fleet mode claims come from the fleet ref, fetched at most once a minute; in team mode from the forge's open merge requests with markers, at most once a minute, kept in the state directory. That is an exception to `add-forge-shipping`'s "the board never calls the forge": in team mode the owner of a change is on the forge, and nowhere else. One cached request a minute is cheap.

A claimed change whose machine isn't connected to Herdr still shows its claim, so an expired claim on a sleeping machine is visible and can be taken over from the board.

### How this fits the other changes

- `ship-claim` and `forge-shipping`: referenced, not restated. Narrowings: team mode's draft at start and "mark ready" at the ship stage (`team-claims`, `Ready at the ship stage`); in fleet mode, the marker's epoch and its rewrite after a take-over (`fleet-claims`, `Forge claim after a take-over`).
- `change-approval`: in team mode the run gate reads only the protected base (`team-claims`, `Approvals only from the protected base`).
- `change-settings` (`add-change-settings`): in team mode `resume` and `rerun` take `settings_hash` from the protected base (`team-claims`, `Resumed settings from the protected base`).
- `change-runs`' slot locks: dispatched runs also take a machine-wide fleet slot (`machine-inventory`, `Machine-wide admission`).
- `task-scope` (`add-task-scope`): the next scope reuses the run's claim (`fleet-claims`, `Starting the next scope` and `Next scope of other runs`; `team-claims`, `Next scope keeps the team claim`).
- `board-data` and `herdr-board`: additions are in `modes-board`, as `ship-board` does.
- `run-status`: `status --all-machines` adds a `machine` field, an addition within the format version.
- `change-runs`' `Parallel limit` is unchanged: per machine. Fleet limits are new and separate.

### Model and code

`formal/fleet.tla` models the fleet ref, claims, renewals, expiry, machines pausing at any stage, take-over of a pushed branch, fenced pushes and pinned merges (see model.md). Its assumptions, each one git operation:

| Model | Code (under `internal/`) |
|---|---|
| `Fetch` | `fleet.fetch`: `git fetch <remote> +refs/tuckerbox/fleet:refs/tuckerbox/fleet` |
| `ForceRelease`: fleet ref, branch lease and release commit in one atomic push | `fleet.release`: `git push --atomic` with both leases |
| `BecomeReady`, `NextScope`: lease push of the fleet ref, with the limit checked in `NextScope` | `fleet.update` on ready, and on `run --tasks` for a ready run |
| `Claim`, `Renew`, `Release`: lease push of the fleet ref | `fleet.update`: build the new commit, `git push --force-with-lease=refs/tuckerbox/fleet:<seen>`; on refusal fetch and re-decide |
| `Claim` with a branch: fleet ref, branch lease and take-over commit in one atomic push | `fleet.takeOver`: `git push --atomic` with both leases |
| `Push`: every branch push, with the fleet ref, atomic | `ship.push` in fleet mode: `git push --atomic` with both leases |
| `SeeGreen`, `Merge`: merge pinned to the commit seen green | `ship.ciState(head)`, then `forge.Merge(sha)` (from `add-forge-shipping`) |
| `Expire` | no code: a reader compares `renewed_at` with its clock |

`OneHolderPerEpoch`, `FencedPush`, `FencedMerge` and `WithinFleetLimit` are named by `fleet-claims` requirements. `scripts/tla-check.sh` already runs every `formal/*.cfg` with its broken variants.

## Risks / Trade-offs

- **[Contention on one ref]** → Every claim and renewal pushes the same ref. Fine for a handful of machines; renewals are every two minutes. If it ever isn't, claims can move to one ref per change and the limit to a separate counter ref, at the cost of a second model.
- **[Remote forbids custom refs]** → Some hosts or push rules refuse refs outside `refs/heads/`. The first claim then fails with the remote's message, and `tuckerbox doctor` in a fleet repository reports whether the fleet ref can be fetched. Fallback, if this turns up in practice: a branch named `tuckerbox/fleet`, which CI may then run on.
- **[Clock skew]** → Affects only when a claim reads as expired; safety comes from fencing. A 1-minute allowance avoids most false expiries.
- **[Laptop sleeps during a step]** → Its claim expires; if nobody takes over, it renews on wake and carries on. If someone did, it blocks at its next renewal and its work stays in its worktree.
- **[Take-over loses work]** → Steps' work on the old machine isn't on the remote. The confirmation says so. Checkpointing is a later change.
- **[Dispatchers race for one machine]** → Capacity reports are a snapshot, so two dispatchers may both place on the last place. The target's machine-wide fleet slots decide; the loser refuses with `<machine> is full` and its dispatch reports it.
- **[Agent signed in but out of quota]** → Sign-in says nothing about limits. The step fails as it does today; dispatch doesn't predict quota.
- **[Team approvals typed by hand]** → Read only from the protected base, which the forge must confirm (see Approvers). Approvals on branches and worktrees don't count.
- **[Take-over commit in history]** → One empty commit per take-over stays in the merge request. It says who took over from whom, which is useful in review.
- **[Forged markers]** → Anyone with push access can open a merge request with a marker naming someone else. In team mode the marker's `by` is checked against the merge request's author from the forge; a mismatch is treated as someone else's claim.

## Migration Plan

- Solo is the default; nothing changes until a repository sets `mode`.
- Fleet: set `mode = "fleet"`, set `fleet_capacity` and `[machines.*]` on each machine, check with `tuckerbox doctor` and `tuckerbox dispatch --dry-run`. Runs already in progress keep running unclaimed; `tuckerbox check` lists them.
- Team: set `mode = "team"`, `ship = "mr"`, `[team] approvers`; each person needs a forge token.
- Rollback: set `mode = "solo"`. Leftover claims are listed by `check` and removed by `tuckerbox claim release --force`; draft merge requests stay for people to close.

## Open Questions

The specs take the stated default for each. A different answer from the owner is a small follow-up, not a change to this design's approach.

- **Headless sign-in:** how should a machine reached only over SSH sign in its agents? Options: an API key in that machine's environment, the CLI's own long-lived token command, or a person signing in once at the machine. Tuckerbox only reports the state either way.
- **The exact sign-in check per CLI** (Claude Code, Codex): which status command, and whether it works without a network.
- **Automatic take-over:** should `dispatch` take over expired claims by itself (`[fleet] takeover = "auto"`)? Off for now because work on the old machine is left behind.
- **Lease length:** 10 minutes, renewed every 2. Shorter frees a crashed machine's change sooner; longer tolerates sleep and bad networks.
- **Team approvals:** protected base is the rule now. Should an approving forge review from a listed login also be accepted, or required?
- **Self-approval in teams:** may the person who approved a change also start it?
- **Fleet inside a team:** should a team member be able to run their own fleet? Not in this change.
