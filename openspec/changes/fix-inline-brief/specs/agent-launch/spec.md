# Spec Delta

## MODIFIED Requirements

### Requirement: Agents by command template
A step SHALL name an agent, defined by a command template with placeholders: `{brief}` (the brief's text), `{brief_file}` (the brief file's path), the working directory, the model, the effort and a file for its final message. Tuckerbox SHALL ship built-in definitions for Claude Code and Codex. Machine or repository config MAY define other agents, which SHALL pass `{brief}` or `{brief_file}`, without permission flags (see repo-config).

#### Scenario: Custom agent
- **WHEN** a repository defines agent `omp` with a command template and a step uses it
- **THEN** Tuckerbox starts that command in the change's worktree with the placeholders filled in

#### Scenario: Custom agent that wants a file
- **WHEN** agent `omp` has `command = ["omp", "--prompt-file", "{brief_file}"]`
- **THEN** Tuckerbox passes the brief file's path, and the brief's text nowhere in the command

#### Scenario: Agent with neither
- **WHEN** a custom agent's command has neither `{brief}` nor `{brief_file}`
- **THEN** `tuckerbox check` exits non-zero and says the command must pass one of them

### Requirement: Step brief
Before starting a step, Tuckerbox SHALL write a brief file, kept for logs and history, and SHALL give the agent the brief's text in its command, so the agent needs no read outside its worktree. The brief holds the step's role prompt, the change id and the paths of its proposal, design, tasks and spec deltas, the result format the final message ends with, and findings from earlier rounds when the step re-runs after a rejection.

#### Scenario: Build after rejection
- **WHEN** the build step starts after the review step rejected round 1
- **THEN** its brief includes round 1's findings

#### Scenario: Agent reads no file
- **WHEN** a step starts a fake agent that may not read the state folder, with `{brief}` in its command
- **THEN** the fake agent receives the whole brief text as that argument, and the brief file is also written

## ADDED Requirements

### Requirement: Long briefs
When the command with the brief inline would be longer than 24,000 characters as a Windows command line, Tuckerbox SHALL, on every platform, send the brief on standard input instead: built-in Claude drops the brief argument and built-in Codex gets `-`. Other agents SHALL get a short instruction to read `{brief}`'s file, and the log SHALL say so. Standard input SHALL be closed after the brief.

#### Scenario: Long brief to Claude
- **WHEN** a Claude step's brief makes the command line longer than 24,000 characters
- **THEN** the command has `-p` with no brief argument, and the agent reads the whole brief on standard input

#### Scenario: Long brief to Codex
- **WHEN** a Codex step's brief makes the command line longer than 24,000 characters
- **THEN** the command ends with `-` in place of the brief, and the agent reads the whole brief on standard input

#### Scenario: Long brief to a custom agent
- **WHEN** a custom agent passes `{brief}` and its brief makes the command line too long
- **THEN** the argument holds an instruction naming the brief file, and the step log says the brief was too long to pass inline

#### Scenario: Windows command line limit
- **WHEN** a step on Windows has a 40,000-character brief
- **THEN** the agent starts, its command line is under 32,767 characters, and it receives the whole brief

### Requirement: Brief text kept out of logs
The step log's start line and the command recorded in run state SHALL show the brief argument as `<brief: N chars, <file>>`, never its text.

#### Scenario: Log line
- **WHEN** a Claude step starts with its brief inline
- **THEN** the log's start line shows `<brief: N chars, …>` where the brief would be, and the brief's text is only in the brief file
