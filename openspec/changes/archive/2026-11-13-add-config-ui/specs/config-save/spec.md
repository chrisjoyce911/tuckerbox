# Spec Delta

## Purpose

Defines how a configuration change is checked and written: the check before saving, protection against overwriting another person's save, and where a repository change goes (the checkout or a pull request).

## ADDED Requirements

### Requirement: Checked before saving
A save SHALL build the new file in memory, merge it with the other config file, and run every check `tuckerbox check` runs on config. It SHALL refuse a save that adds a problem, name each problem's file and key, and write nothing. Problems the file already had SHALL be listed and SHALL NOT block. Model: `formal/configsave.tla`, `SavesAddNoProblem`.

#### Scenario: Removing a step another step needs
- **WHEN** the review step has `on_reject = "build"` and the user runs `tuckerbox config set --unset steps.build`
- **THEN** it refuses, names `steps.review.on_reject`, and the file is unchanged

#### Scenario: Fixing one of two problems
- **WHEN** repository config has two problems and a save fixes one of them
- **THEN** the save is written and the remaining problem is listed

### Requirement: Problems in changes do not block
A save SHALL NOT be refused for problems it causes in active changes, such as a change override that `[overrides]` no longer allows. It SHALL list each change and key affected, so the person can fix the change.

#### Scenario: Narrowing the allow-list
- **WHEN** change `add-login` sets `[steps.build] agent = "codex"` and the user runs `tuckerbox config set overrides.agents=claude`
- **THEN** the save is written and the output says `add-login`'s `step.build.agent` is no longer allowed

### Requirement: No lost saves
`tuckerbox config set` SHALL accept `--if-match <revision>`. It SHALL take the file's lock, re-read the file, and refuse with "changed since read" if the revision differs. The check and the write SHALL use the file as read under the lock. Model: `formal/configsave.tla`, `NoLostSave`.

#### Scenario: Two people edit at once
- **WHEN** the Settings view read revision r1, then a terminal saved `max_parallel=3`, then the view saves `--if-match r1`
- **THEN** the view's save is refused as changed since read, and the file keeps `max_parallel = 3`

### Requirement: Atomic writes that keep the file's shape
A save SHALL write a temporary file in the same folder and rename it over the config file, so nothing reads half a file. It SHALL change only the lines of the keys it sets or removes, and keep comments, key order and other keys.

#### Scenario: Comment kept
- **WHEN** a comment sits above `[steps.review]` and the user sets `steps.review.effort=high`
- **THEN** the comment is still there and only the `effort` line differs

### Requirement: Where a repository save goes
Without `--local` or `--pr`, a repository save SHALL open a pull request when this machine has a token for the repository's forge, and otherwise save to the checkout and say why. `--pr` SHALL refuse rather than save to the checkout. The output SHALL say where the save went.

#### Scenario: No token on this machine
- **WHEN** the user runs `tuckerbox config set max_parallel=3` and this machine has no token for the remote's host
- **THEN** it saves to the checkout and says no token was found for that host and that nothing was committed

#### Scenario: Pull request demanded but not possible
- **WHEN** the user runs `tuckerbox config set --pr max_parallel=3` and this machine has no token for the remote's host
- **THEN** it exits non-zero and no file changes

### Requirement: Saving to the checkout
A local repository save SHALL write `.tuckerbox/config.toml` in the repository's main checkout and SHALL NOT commit. It SHALL say the change counts elsewhere only once committed and pushed, and that runs in existing worktrees keep their own copy.

#### Scenario: Local save
- **WHEN** the user runs `tuckerbox config set --local max_parallel=3`
- **THEN** the main checkout's `.tuckerbox/config.toml` has `max_parallel = 3`, nothing is committed, and the output says so

### Requirement: Saving as a pull request
A pull request save SHALL fetch the base branch and judge the edit and `--if-match` against the base branch's file. It SHALL commit the edit on a new branch in a temporary worktree, push without force, open a merge request ready for review, and remove the worktree. It SHALL leave the checkout unchanged and print the link.

#### Scenario: Pull request
- **WHEN** the user runs `tuckerbox config set --pr max_parallel=3` and the forge is reachable
- **THEN** a new branch holds one commit changing only that line, a merge request is open for it, its link is printed, and the checkout's files are unchanged

#### Scenario: Base branch moved
- **WHEN** the view read revision r1 from the base branch and someone merged a config change before the save
- **THEN** the pull request save is refused as changed since read, and no branch is pushed

### Requirement: People merge config pull requests
Tuckerbox SHALL NOT merge a config pull request, whatever the ship mode, and it SHALL NOT need change approval. The repository's own review rules decide it.

#### Scenario: Merge ship mode
- **WHEN** the repository has `ship = "merge"` and a config pull request's checks are green
- **THEN** Tuckerbox leaves the pull request open

### Requirement: Machine saves
A machine save SHALL write machine config in this machine's config folder, creating it when absent, and SHALL never commit or push. Tokens SHALL go to `credentials.toml` in that folder, created readable only by the user.

#### Scenario: First token on a new machine
- **WHEN** no credentials file exists and the user enters a token for `github.com`
- **THEN** `credentials.toml` is created holding only that token, readable only by the user, and machine `config.toml` is unchanged
