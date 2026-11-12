# config-cli Specification

## Purpose
Defines `tuckerbox config`: reading a repository's and this machine's effective configuration with where each value comes from, and changing keys from the command line.

## Requirements

### Requirement: Key catalogue
`tuckerbox config show` SHALL list every key `tuckerbox check` knows, with its effective value, its source (`repo`, `machine` or `default`) and its scope (`repo`, `machine` or `either`). The list SHALL come from the same key table `tuckerbox check` uses, so a key a later change adds appears with no other work.

#### Scenario: Repository value wins
- **WHEN** machine config sets `max_parallel = 4` and repository config sets `max_parallel = 2`
- **THEN** `tuckerbox config show` lists `max_parallel` as `2` from `repo`, with scope `either`, and shows that machine config sets `4`

#### Scenario: Built-in default
- **WHEN** neither config file sets `board.columns`
- **THEN** `tuckerbox config show` lists `board.columns` as `compact` from `default`

### Requirement: This machine only
`tuckerbox config show` SHALL list machine-scope keys in their own section, headed with this machine's name and "this machine only". It SHALL read them only from machine config and the credentials file.

#### Scenario: Permission profile
- **WHEN** machine config defines `[permissions.edit]`
- **THEN** `tuckerbox config show` lists `permissions.edit` under this machine's "this machine only" heading, from `machine`

### Requirement: Config as JSON
`tuckerbox config show --json` SHALL give, for each key: its name, effective value, source, scope, the file it came from, and its allowed values when they are fixed. It SHALL also give each config file's path and revision (the SHA-256 of its bytes, or none when the file is absent).

#### Scenario: Revision for a later save
- **WHEN** a tool reads `tuckerbox config show --json`
- **THEN** it gets the revision of the repository file and of the machine file, which `tuckerbox config set --if-match` accepts

### Requirement: Reading one key
`tuckerbox config get <key>` SHALL print the effective value and source of one key, or of every key in a table such as `steps.review`. `--json` SHALL give the same fields as `show --json`. An unknown key SHALL exit non-zero and name the nearest known key.

#### Scenario: Step setting
- **WHEN** repository config sets `[steps.review] effort = "high"`
- **THEN** `tuckerbox config get steps.review.effort` prints `high (repo)`

#### Scenario: Misspelt key
- **WHEN** the user runs `tuckerbox config get max_paralel`
- **THEN** it exits non-zero and suggests `max_parallel`

### Requirement: Secrets are never shown
For a secret key (a forge token), `show` and `get` SHALL print only `set` or `not set` and where the token in use comes from: the credentials file, a variable's name, `gh` or `glab`. They SHALL never print the value, in text or JSON.

#### Scenario: Token set
- **WHEN** the credentials file holds a token for `github.com`
- **THEN** `tuckerbox config show` lists `forges."github.com".token` as `set` from the credentials file, and no part of the token appears in the output

### Requirement: Setting keys
`tuckerbox config set <key>=<value>...` SHALL set one or more keys in one save, and `--unset <key>` SHALL remove one. Without `--machine` it SHALL write repository config; with `--machine`, machine config. A value SHALL be parsed as its key's type; a list is comma-separated. `--dry-run` SHALL check and say where it would save, and write nothing.

#### Scenario: Two keys in one save
- **WHEN** the user runs `tuckerbox config set max_parallel=3 steps.review.max_rounds=2`
- **THEN** both keys change in repository config in one save, and the file's other lines and comments are unchanged

#### Scenario: Back to the machine default
- **WHEN** the user runs `tuckerbox config set --unset steps.review.effort` and machine config sets that effort
- **THEN** the repository line is removed and `config get steps.review.effort` shows the machine's value from `machine`

#### Scenario: Dry run
- **WHEN** the user runs `tuckerbox config set --dry-run max_parallel=3`
- **THEN** it prints the check result and where it would save, and no file changes

### Requirement: Keys in the wrong file
`tuckerbox config set` SHALL refuse a machine-scope key without `--machine` and a repo-scope key with `--machine`. It SHALL write nothing and say which file the key belongs in.

#### Scenario: Profile in repository config
- **WHEN** the user runs `tuckerbox config set permissions.edit.allowed_remotes=*` without `--machine`
- **THEN** it refuses, says permission profiles belong in machine config, and writes nothing

#### Scenario: Approval rule on the machine
- **WHEN** the user runs `tuckerbox config set --machine require_approval=false`
- **THEN** it refuses, says `require_approval` belongs in repository config, and writes nothing

### Requirement: Entering a token
`tuckerbox config set --machine forges.<host>.token` SHALL read the token from a prompt that does not echo, or from standard input when it is not a terminal. It SHALL refuse a token given in the arguments. It SHALL say only that the token was saved, and `--unset` SHALL remove it.

#### Scenario: Token in the arguments
- **WHEN** the user runs `tuckerbox config set --machine forges.github.com.token=abc123`
- **THEN** it refuses, says to leave out the value and enter it at the prompt, writes nothing, and does not print `abc123`

### Requirement: Editing a config file directly
`tuckerbox config edit` SHALL open repository config, and `config edit --machine` machine config, in the editor `tuckerbox edit` uses. When the editor exits it SHALL run `tuckerbox check` and print each problem. It SHALL NOT commit.

#### Scenario: Syntax error after an edit
- **WHEN** the user saves the repository config with a TOML syntax error and closes the editor
- **THEN** `tuckerbox config edit` exits non-zero and names the file and line
