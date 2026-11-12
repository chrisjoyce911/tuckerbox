# Spec Delta

## Purpose

Defines the list of machines an owner can run work on, how Tuckerbox reaches them, what each machine reports about itself (capacity, agents, sign-in, checkout), and status across machines.

## ADDED Requirements

### Requirement: Machine list
Machine config MAY list other machines as `[machines.<name>]`, each with exactly one of `ssh` (an SSH destination) or `hc` (a herdr-core machine name). The list SHALL be read only from machine config. A repository config that holds `[machines]` SHALL fail validation.

#### Scenario: Machine reached by herdr-core
- **WHEN** machine config has `[machines.lab] hc = "lab"`
- **THEN** Tuckerbox runs commands on `lab` with `hc run --on lab`

#### Scenario: Both ways given
- **WHEN** machine config has `[machines.mini]` with both `ssh` and `hc`
- **THEN** `tuckerbox check` exits non-zero and names `machines.mini`

### Requirement: Machine capacity
Machine config `fleet_capacity` SHALL say how many fleet runs this machine takes at once, across repositories, and default to 0 (it takes none). Runs a person starts on the machine directly SHALL NOT count against it. `max_parallel` SHALL still apply per repository.

#### Scenario: Machine not in the fleet
- **WHEN** machine `mini` has no `fleet_capacity`
- **THEN** it reports capacity 0 and `tuckerbox dispatch` places nothing on it

### Requirement: Machine report
`tuckerbox machine --json [--for <remote>]` SHALL print the machine's name, Tuckerbox version, operating system, capacity and fleet runs, each agent CLI with whether it is installed and signed in, and, with `--for`, whether this machine has a checkout of that remote and its path. It SHALL NOT print tokens or permission flags.

#### Scenario: Codex signed out
- **WHEN** Codex is installed on `mini` but not signed in
- **THEN** the report lists `codex` with installed `true` and signed_in `false`

#### Scenario: No checkout
- **WHEN** `tuckerbox machine --json --for github.com/sam/app` runs on a machine without that repository
- **THEN** the report says checkout `false`

### Requirement: Agent sign-in check
`tuckerbox doctor` SHALL check whether each installed built-in agent is signed in, without starting a session or using model tokens, and report `signed in`, `signed out` or `unknown`. An agent without a known check SHALL be `unknown`. It SHALL NOT sign in or change agent config.

#### Scenario: Unknown sign-in
- **WHEN** a custom agent `omp` is installed
- **THEN** `doctor` reports `omp` sign-in as `unknown` and still exits 0

### Requirement: Running commands on other machines
Tuckerbox SHALL run a command on a listed machine with `ssh <destination> -- tuckerbox ...` or `hc run --on <name> -- tuckerbox ...`, without a terminal, and with a 30 s timeout for reports. An unreachable machine SHALL be reported by name and SHALL NOT stop work on the others. It SHALL send no token or credential.

#### Scenario: Machine asleep
- **WHEN** `mini` doesn't answer within the timeout
- **THEN** the command names `mini` as unreachable and goes on with the other machines

### Requirement: Status across machines
`tuckerbox status --all-machines [--json]` SHALL list this machine's runs and every listed machine's runs, each labelled with its machine, and name unreachable machines. The JSON form SHALL keep `status --json`'s format and version, adding a `machine` field to each run.

#### Scenario: Runs on two machines
- **WHEN** `add-login` runs on `studio` and `add-users` on `mini`, and the user runs `tuckerbox status --all-machines` on `studio`
- **THEN** both runs are listed, labelled `studio` and `mini`
