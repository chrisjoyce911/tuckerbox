# Design

## Context

See proposal.md for why. Today `internal/agent` writes the brief to the
state folder in the worktree's git folder. It gives built-in agents
`{prompt}`, a one-line "Read the Tuckerbox brief at <path>", and custom
agents `{brief}`, the path. The agent's standard input is empty.

## Goals / Non-Goals

**Goals:**
- An agent can start its step with no read outside its worktree.
- The same behaviour on macOS, Linux and Windows, tested on all three.

**Non-Goals:**
- Standard input for custom agents. They get the text inline, or the file
  path when it is too long. A later change can add a setting for it if a
  custom agent needs it.
- Moving the state folder into the worktree. Run state must stay out of
  the tree (`change-runs`).

## Decisions

### `{brief}` is the text, `{brief_file}` the path

The built-in templates change `{prompt}` to `{brief}`. `{prompt}` goes
away: it only existed to point at the file. `tuckerbox check` requires a
custom agent to pass `{brief}` or `{brief_file}`.

This breaks custom agents written as `--prompt-file {brief}`: they would
get the text where a path was expected. Tuckerbox has no released binaries
and this repository defines no custom agent, so the clean names are worth
it now. The starter config and README change to `{brief_file}`. See Open
Questions for the alternative.

### One length limit on every platform

Windows limits a whole command line to 32,767 UTF-16 characters. Linux
limits one argument to 128 KiB, and macOS the whole environment and
arguments to about 1 MiB. Tuckerbox measures the command as Windows would
quote it (`syscall.EscapeArg` rules, the same code on every platform) and
switches above 24,000 characters. That leaves room for the program path and
for flags a permission profile adds. Using one limit everywhere means a
brief that works on a Mac also works on Windows, and the tests are the same
on all three.

### Standard input for long briefs

- **Claude:** `claude -p` with no prompt argument reads the prompt from
  standard input. The argument that was `{brief}` is left out.
- **Codex:** `codex exec -` reads the prompt from standard input
  (`codex exec --help`). The argument becomes `-`.
- **Custom agents:** the `{brief}` argument becomes a short instruction:
  read the brief at `<file>`; if you can't read it, end with a `blocked`
  result naming the file. The log notes the fallback. A custom agent that
  wants no limit should use `{brief_file}`.

Tuckerbox writes the brief to the pipe from a goroutine and closes it, so
an agent that reads slowly can't block the start, and one that never reads
still gets end of file. When the brief is inline, standard input stays
empty as today.

### Logs and run state

The command is logged at step start and kept in `Outcome.Argv`. With the
brief inline, that would copy the whole brief into the log on every step.
Both show `<brief: N chars, <file>>` in its place. The brief file stays
the record of what the agent was told.

### Model

None (model.md): one process starts one agent; nothing new is shared.

## Risks / Trade-offs

- **[Brief visible in process lists]** → Other local users can see a
  process's arguments. The brief holds the role prompt, paths and review
  findings, which are already in the repository and its run state. It
  never holds tokens (`forge-access`).
- **[Agent CLIs change how they read standard input]** → The integration
  tests that run the real CLIs, where installed, cover both forms. The
  fake agent tests cover the contract.
- **[Custom agents break]** → `tuckerbox check` can't tell a path use from
  a text use. The README and starter config say to use `{brief_file}` for
  a file, and the error a broken agent gives names the bad path.

## Migration Plan

- Built-in agents change with no config change.
- A custom agent that reads a file changes `{brief}` to `{brief_file}`.
- Machine profiles may drop `--add-dir` for the git folder.
- Rollback: revert; the brief file is still written, so old templates work.

## Open Questions

- **Keep `{brief}` as the path for compatibility?** The alternative is a new
  `{brief_text}` for the text, with `{brief}` unchanged. Nothing else in
  this change would differ.
