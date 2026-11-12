# Proposal

## Why

In a real run, `add-priority`'s Claude builder couldn't read its brief.
The brief lives in the worktree's git folder
(`<repo>/.git/worktrees/<worktree>/tuckerbox/briefs/`), which is outside the
worktree. The machine's permission profile no longer gave Claude
`--add-dir` for `.git`, so the read was denied. The agent rightly refused
to work around it, and the step went nowhere.

An agent shouldn't need any access outside its worktree just to learn what
to do. The brief is a few kilobytes of text, so it can go in the command
itself.

## What Changes

- **`{brief}` is the brief's text.** The built-in Claude and Codex agents
  get it as their prompt argument. **BREAKING** for custom agents that
  used `{brief}` as a file path: they now use `{brief_file}`.
- **`{brief_file}` is the brief file's path,** for custom agents that want
  a file. A custom agent must pass `{brief}` or `{brief_file}`.
- **The brief file is still written,** for logs, history and
  `TUCKERBOX_BRIEF`.
- **Long briefs go on standard input.** Above 24,000 characters as a
  Windows command line (Windows allows 32,767), on every platform:
  Claude gets `-p` with no prompt argument and Codex gets `-`, and both
  read the brief from standard input. A custom agent gets a short
  instruction to read the brief file instead, and the log says so.
- **Logs and run state** show the brief argument as
  `<brief: N chars, <file>>`, not its text.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `agent-launch`: `Agents by command template` (the `{brief}` and
  `{brief_file}` placeholders), `Step brief` (the text goes in the
  command), and new requirements `Long briefs` and `Brief text kept out of
  logs`.

## Impact

- `internal/agent`: template expansion, the built-in templates, standard
  input for the agent process, and the logged command.
- `internal/config`: the placeholder list, the check that a custom agent
  passes `{brief}` or `{brief_file}`, and the starter config's comment.
- README: the custom agent example uses `{brief_file}`.
- Machine profiles no longer need `--add-dir` for the git folder for agents
  to read their brief.
