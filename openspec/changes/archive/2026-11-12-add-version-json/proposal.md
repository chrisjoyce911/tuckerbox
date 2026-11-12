# Proposal

## Why

The Herdr board and `tuckerbox doctor` on other machines need to compare
Tuckerbox versions. Today `tuckerbox version` prints text only. This is also
the first change Tuckerbox runs on itself, so it is kept small.

## What Changes

- `tuckerbox version --json` prints the version, commit, Go version, OS and
  architecture as one JSON object.
- Plain `tuckerbox version` is unchanged.

## Capabilities

### New Capabilities

- `version-info`: what `tuckerbox version` reports and in which forms.

### Modified Capabilities

None.

## Impact

- `internal/version` and the `version` command in `internal/cli`.
- No new dependencies.
