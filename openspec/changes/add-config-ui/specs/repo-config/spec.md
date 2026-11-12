# Spec Delta

## ADDED Requirements

### Requirement: Every key has one scope
Each config key SHALL have one scope: `repo` (repository config only), `machine` (machine config only) or `either` (a machine default the repository overrides). `tuckerbox check`, `tuckerbox config` and the Settings view SHALL take scopes from one table, so they never disagree about where a key belongs.

#### Scenario: Scopes agree
- **WHEN** `tuckerbox config show --json` gives a key scope `machine`
- **THEN** `tuckerbox check` fails for a repository config that sets that key, and names it

### Requirement: Stored forge tokens
Tuckerbox SHALL read stored forge tokens from `credentials.toml` in the machine config folder, as `[forges."<host>"] token`, and from no other file. It SHALL try that token after `token_env` and before the default variables. This adds to `forge-access`'s token order once `add-forge-shipping` is archived.

#### Scenario: Stored token used first
- **WHEN** `credentials.toml` holds a token for `github.com` and `GITHUB_TOKEN` is also set
- **THEN** Tuckerbox uses the stored token, and `doctor` reports its source as the credentials file

#### Scenario: Token in machine config
- **WHEN** machine `config.toml` contains `[forges."github.com"] token = "..."`
- **THEN** `tuckerbox check` exits non-zero, says tokens belong in the credentials file through `tuckerbox config set`, and does not print the value

### Requirement: Credentials file is private
On macOS and Linux, Tuckerbox SHALL refuse to read `credentials.toml` when its group or others can read it. It SHALL say how to fix the file's mode and SHALL NOT print its contents.

#### Scenario: File readable by others
- **WHEN** `credentials.toml` has mode 0644
- **THEN** `tuckerbox check` exits non-zero, names the file, says to run `chmod 600` on it, and uses no token from it
