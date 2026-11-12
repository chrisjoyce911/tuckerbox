# Proposal

## Why

`add-herdr-board` was archived with two tasks a person has to do by hand,
which no build step can tick: a live trial of the board on two connected
machines, and screenshots for the README. This change keeps them so they
aren't lost.

## What Changes

- Run `herdr-app check` and try the board with runs on two connected
  Herdr machines, including edit, tick and approve, and fix what the trial
  finds (as separate changes if they change behaviour).
- Add screenshots of the board and card detail to the README.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. No behaviour changes: this is a manual check and documentation.

## Impact

- README images only.
