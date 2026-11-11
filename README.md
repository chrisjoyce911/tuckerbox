# Tuckerbox

Tuckerbox runs [OpenSpec](https://github.com/Fission-AI/OpenSpec) changes through coding agents: plan, build, review and ship, several changes at once, each in its own git worktree. Each repository decides how its own work runs, in a config file committed with it.

It's a single Go binary for macOS, Linux and Windows. It works on its own, and it fits into [Herdr](https://herdr.dev), where it shows a board of your changes and their agents.

> **Status:** design stage. Nothing is built yet. The plan is in [`openspec/changes/`](openspec/changes/):
> - [`add-core-runner`](openspec/changes/add-core-runner/proposal.md): per-repo workflows, local runs, agents, status
> - [`add-herdr-board`](openspec/changes/add-herdr-board/proposal.md): run history, dependencies, and a kanban board in Herdr

Named after the [Dog on the Tuckerbox](https://en.wikipedia.org/wiki/Dog_on_the_Tuckerbox), an Australian working dog that never left its post.

## Licence

Apache-2.0. See [LICENSE](LICENSE).
