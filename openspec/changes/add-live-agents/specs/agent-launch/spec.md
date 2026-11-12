# Spec Delta

## MODIFIED Requirements

### Requirement: Headless execution and logs
Agents SHALL run in one of two backends: headless, as child processes of Tuckerbox (`claude -p`, `codex exec`), or in herdr-core terminals (see `agent-terminals`). In the headless backend their output SHALL be saved to a per-step log file that `tuckerbox logs <change> [step]` can show and follow.

#### Scenario: Following a running step
- **WHEN** the user runs `tuckerbox logs fix-login --follow` while the build step runs
- **THEN** the build agent's output appears as it is written

#### Scenario: Headless fallback
- **WHEN** the terminal backend isn't in use on the machine
- **THEN** each step's agent runs as a child process of Tuckerbox, with no terminal, as before
