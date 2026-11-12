# Design

## Context

`internal/version` already collects the version, commit, Go version and
platform for the text output.

## Decisions

- Marshal a small struct with `encoding/json`; field names are lower case and
  stable, because other machines' boards will read them.
- Keep the existing text format byte for byte.

## Risks / Trade-offs

- None of note.
