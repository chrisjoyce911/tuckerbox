# Tuckerbox

Tuckerbox runs [OpenSpec](https://github.com/Fission-AI/OpenSpec) changes through coding agents: plan, build, review and ship, several changes at once, each in its own git worktree. Each repository decides how its own work runs, in a config file committed with it.

It's a single Go binary for macOS, Linux and Windows. It works on its own, and it fits into [Herdr](https://herdr.dev), where it shows a board of your changes and their agents.

> **Status:** early development. Only `tuckerbox version` and `tuckerbox doctor` work so far; runs, config, agents and status are not built yet. The plan is in [`openspec/changes/`](openspec/changes/):
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
tuckerbox version   # version, commit, Go version and platform
tuckerbox doctor    # checks git, openspec and the agent CLIs (claude, codex)
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
