# agent-launch Specification

## Purpose
Defines how a workflow step starts an agent command-line tool, what the agent is given, and how it reports back, independent of which agent is used.

## Requirements

### Requirement: Agents by command template
A step SHALL name an agent. An agent is defined by a command template with placeholders for the brief file, the working directory, the model, the effort and a file for its final message. Tuckerbox SHALL ship built-in definitions for Claude Code and Codex. Machine or repository config MAY define other agents, without permission flags (see repo-config).

#### Scenario: Custom agent
- **WHEN** a repository defines agent `omp` with a command template and a step uses it
- **THEN** Tuckerbox starts that command in the change's worktree with the placeholders filled in

### Requirement: Step brief
Before starting a step, Tuckerbox SHALL write a brief file the agent reads first. The brief contains:
- the step's role prompt (from the repository, or the built-in default for that role)
- the change id and the paths of its proposal, design, tasks and spec deltas
- the result format, and that the agent ends with it as its final message
- findings from earlier rounds, when the step is re-run after a rejection

#### Scenario: Build after rejection
- **WHEN** the build step starts after the review step rejected round 1
- **THEN** its brief includes round 1's findings

### Requirement: Result contract
An agent SHALL end with its result as its final message: a JSON object, fenced or bare, with `change`, `step` and `round` from the brief, `status` (`done`, `rejected`, `blocked`, `failed`), a `summary` and optional `findings` (`severity`, `file`, `detail`). Tuckerbox SHALL write the result file itself. A result file the agent writes is also accepted, and wins if they differ. Only a well-formed result for this change, step and round is accepted. Model: `formal/runs.tla`, `StaleNeverAccepted`.

#### Scenario: Result in the final message
- **WHEN** a reviewer in a read-only sandbox ends with a valid result for its step and round
- **THEN** Tuckerbox accepts it and writes the step's result file

#### Scenario: Malformed final message
- **WHEN** the agent exits with no result file and a final message holding no well-formed result
- **THEN** the step fails, and the log and run record why

#### Scenario: Result file and final message differ
- **WHEN** the agent writes a result file and ends with a different valid result
- **THEN** Tuckerbox uses the result file and logs the difference

#### Scenario: Stale result from an earlier round
- **WHEN** a result names round 1 while the step is on round 2
- **THEN** Tuckerbox ignores it and keeps waiting for the round 2 result

### Requirement: Headless execution and logs
In this change, agents SHALL run headless as child processes of Tuckerbox. Their output SHALL be saved to a per-step log file that `tuckerbox logs <change> [step]` can show and follow.

#### Scenario: Following a running step
- **WHEN** the user runs `tuckerbox logs fix-login --follow` while the build step runs
- **THEN** the build agent's output appears as it is written

### Requirement: Permission profiles
An agent SHALL run with the permission flags of the step's permission profile, as granted by machine config (see repo-config). With no profile, it SHALL run with the agent's own defaults. Tuckerbox SHALL NOT add flags that bypass the agent's permission prompts unless a machine profile grants them.

#### Scenario: No profile set
- **WHEN** a step names no permission profile
- **THEN** the agent command contains no permission-bypass flags
