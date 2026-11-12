# Tuckerbox

Tuckerbox runs [OpenSpec](https://github.com/Fission-AI/OpenSpec) changes through coding agents: plan, build, review and ship, several changes at once, each in its own git worktree. Each repository decides how its own work runs, in a config file committed with it.

It's a single Go binary for macOS, Linux and Windows. It works on its own, and it fits into [Herdr](https://herdr.dev), where it shows a board of your changes and their agents.

> **Status:** early development. Local runs work: `run`, `status`, `logs`, `stop`, `resume` and `rerun`, as well as `version`, `doctor`, `init` and `check`. Shipping (pushing, MRs) and Herdr come later. The plan is in [`openspec/changes/`](openspec/changes/):
> - [`add-core-runner`](openspec/changes/add-core-runner/proposal.md): per-repo workflows, local runs, agents, status
> - [`add-herdr-board`](openspec/changes/add-herdr-board/proposal.md): run history, dependencies, and a kanban board in Herdr

## Install

Tuckerbox needs Go 1.23 or later to build. There are no released binaries yet.

```sh
go install github.com/chrisjoyce911/tuckerbox/cmd/tuckerbox@latest
```

Or build from a checkout, with the version and commit stamped in:

```sh
CGO_ENABLED=0 go build \
  -ldflags "-X github.com/chrisjoyce911/tuckerbox/internal/version.Version=$(git describe --tags --always) \
            -X github.com/chrisjoyce911/tuckerbox/internal/version.Commit=$(git rev-parse --short HEAD)" \
  -o tuckerbox ./cmd/tuckerbox
```

The binary is built without cgo, so it is a single static file on macOS, Linux and Windows.

## Use

```sh
tuckerbox version   # version, commit, Go version and platform (--json for tools)
tuckerbox doctor    # checks git, openspec and the agent CLIs (claude, codex)
tuckerbox init      # writes a commented starter .tuckerbox/config.toml
tuckerbox herdr install  # installs or updates the board mod in the Herdr app
                         # (--keys: a key to open and hide it; --drawer: a peek drawer)
tuckerbox herdr uninstall  # undoes exactly what herdr install did
tuckerbox check     # validates it, merged with this machine's config
```

Run changes from inside the repository:

```sh
tuckerbox run fix-login            # one change, in the background
tuckerbox run --all                # every valid active change
tuckerbox run --foreground fix-login   # one change, attached to this terminal
tuckerbox status                   # every run on this machine (--json for dashboards)
tuckerbox board                    # this repository’s changes, approval and workflow columns
tuckerbox board --change fix-login --json  # board plus proposal, tasks, rounds and history
tuckerbox board --json --repo github.com/you/shop  # this machine's checkout of that repository, from anywhere
tuckerbox logs fix-login --follow  # watch the running step's agent
tuckerbox stop fix-login           # stop the agent; the run is marked stopped
tuckerbox resume fix-login         # continue a stopped or blocked run
tuckerbox rerun fix-login review   # start again from a step
tuckerbox history fix-login        # every event of the run, oldest first (--json for tools)
```

Fix a change's OpenSpec files through Tuckerbox, so no run starts meanwhile:

```sh
tuckerbox edit fix-login design      # proposal, design, model, tasks or spec:<capability>
tuckerbox tasks fix-login tick 2.1   # or untick; changes only that checkbox
tuckerbox validate fix-login         # openspec validate --strict (--json for tools)
```

They work in the change's worktree if it has one on this machine, else in the checkout, and refuse while its run is queued or running. They never commit. The editor is `editor` in machine config, else `$VISUAL`, else `$EDITOR`; a GUI editor must wait for the file to close (`code --wait`). `edit` validates strictly when the editor exits.

A change must be approved before `tuckerbox run` starts it:

```sh
tuckerbox approve fix-login     # validates strictly, then records the approval
git add openspec/changes/fix-login/tuckerbox.toml
git commit -m "Approve fix-login" && git push
tuckerbox run fix-login
tuckerbox unapprove fix-login   # withdraw it (also uncommitted)
```

`approve` writes an `[approval]` table (a hash of the proposal, design, model, tasks and spec deltas, your git `user.name` and the time) to `openspec/changes/<change>/tuckerbox.toml`. It never commits, and the run judges the files it will start from: the change's worktree if it has one, else its branch or the base branch after a fetch. So approve, commit and push before `tuckerbox run`, or it refuses and says the approval isn't committed. Ticking tasks keeps the approval; any other edit to those files makes it stale on every machine, and the change needs approving again. `tuckerbox run --all` skips changes that aren't approved and lists them. Resume and re-run don't check approval again. To turn the gate off for a repository, set it in the repository config (machine config may not):

```toml
require_approval = false
```

Each change gets its own worktree (by default `../<repo>-worktrees/<change>`) and branch, made from the remote's default branch after a fetch. Existing worktrees are reused as they are: Tuckerbox never resets, cleans or stashes them. At most `max_parallel` changes run at once; the rest are `queued` and start in admission order (see Priority below), which without priorities is the order requested. With `ship = "local"`, a run that finishes commits what is left on its branch and becomes `ready`; nothing is pushed. A change must pass `openspec validate` and be committed on the base branch.

A change can wait for others: list them in `openspec/changes/<change>/tuckerbox.toml` as `depends_on = ["add-users"]`. Its run stays `queued (waiting on add-users (build))`, without taking a slot, until each dependency is `ready` on this machine or archived. `tuckerbox check` reports dependencies that name no active or archived change, and cycles.

### Priority

You decide which change goes next by ranking them:

```sh
tuckerbox priority                 # the active changes in priority order (--json for tools)
tuckerbox priority add-login top   # move a change to position 1
tuckerbox priority add-login 3     # to position 3 (past the end means last)
tuckerbox priority add-login up    # one place up (or down)
```

The rank is `priority = <n>` in `openspec/changes/<change>/tuckerbox.toml`: lower first, and a change without one comes after every ranked change. People use positions; Tuckerbox picks the numbers, with gaps of 1000, so a move usually rewrites one line of one file, and renumbers the ranked changes only when no gap is left. `tuckerbox check` reports a `priority` that isn't a positive integer. On the board, drag a card within its column to reorder it.

`priority` writes the repository checkout on this machine, never a change's worktree, even while the change is queued or running, and never commits. Queued runs on this machine follow the new order at once; commit and push the files for other machines to see it. `tuckerbox.toml` isn't part of the approval hash, so a priority never makes an approval stale. `priority`, `approve` and `unapprove` take a short repository-wide lock around each rewrite, so they never lose each other's edits.

When a slot frees, the queued run first in admission order takes it: lowest priority first, where a change counts as urgent as the most urgent active change that depends on it, then the one requested first. Age doesn't count, so with no priorities the order is the order requested. `tuckerbox run --all` requests changes oldest first and starts them in that order. A higher priority never stops a running change; it only decides who takes the next free slot. Edits to `priority` made outside Tuckerbox (an editor, `git pull`) count from the next admission.

Run state is kept in the worktree's git directory (`$(git rev-parse --git-dir)/tuckerbox/`), never in the tree. Next to the current state (`run.json`), each run appends its events (started, steps, hand-offs, rejections with findings, stopped, resumed, ready) to `history.jsonl` there; the format is documented in [`internal/history`](internal/history/history.go). `status` finds runs through a machine index in `$XDG_STATE_HOME/tuckerbox/`, `~/.local/state/tuckerbox/` on macOS and Linux, or `%LocalAppData%\tuckerbox\` on Windows.

A repository's workflow lives in `.tuckerbox/config.toml`, committed with it. Machine defaults go in `$XDG_CONFIG_HOME/tuckerbox/config.toml` if that variable is set, otherwise `~/.config/tuckerbox/config.toml` on macOS and Linux and `%AppData%\tuckerbox\config.toml` on Windows. Repository values win, except that permission profiles (the flags that let agents run without prompts, and which repository remotes may use them) and the editor can only be set on the machine:

```toml
# machine config
[permissions.edit]
allowed_remotes = ["github.com/you/*"]
args = { claude = ["--permission-mode", "acceptEdits"] }
```

A step in the repository then asks for it by name with `permissions = "edit"`.

The board defaults to Proposed, Approved (when approval is required), In progress,
Ready and Done. In-progress cards carry their step or `queued` as a badge.
To show Queued and one column per workflow step, set this in repository config
(or machine config as a default; repository values win):

```toml
[board]
columns = "steps" # "compact" is the default
```

Agents report by ending with a small JSON result as their final message; Tuckerbox records it in the step's result file, so an agent needs no write access outside its worktree and a reviewer can run read-only (for Codex, `--sandbox read-only` in a machine profile). Tuckerbox reads Claude's final message from its stream-json output and Codex's from the file it writes with `-o`. A custom agent can pass `{lastmessage}`, a file its CLI writes the final message to; otherwise its standard output is read:

```toml
[agents.myagent]
command = ["myagent", "--prompt-file", "{brief}", "--last-message", "{lastmessage}"]
```

`doctor` exits non-zero only when a required tool (`git` or `openspec`) is missing. Agent CLIs are optional. It does not yet check whether an agent is signed in, and says `sign-in not checked`.

## Develop

```sh
go vet ./...
go test -race ./...        # the race detector needs cgo
scripts/tla-check.sh       # TLC on formal/: needs bash, Java 11+ and curl
```

CI runs the same checks on Linux, macOS and Windows ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)).

Named after the [Dog on the Tuckerbox](https://en.wikipedia.org/wiki/Dog_on_the_Tuckerbox), an Australian working dog that never left its post.

## Licence

Apache-2.0. See [LICENSE](LICENSE).
