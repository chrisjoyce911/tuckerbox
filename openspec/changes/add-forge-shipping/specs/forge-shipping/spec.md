# Spec Delta

## Purpose

Defines what a run does after its last workflow step when the repository ships through a forge: archive the change, push its branch, open a merge request, and stop for people or merge it.

## ADDED Requirements

### Requirement: Ship modes
Repository config `ship` SHALL accept `local`, `mr` and `merge`. With `mr` or `merge`, a run whose last step reports `done` SHALL enter state `shipping` and follow the steps in this capability. `local` SHALL keep its current behaviour.

#### Scenario: Last step done in mr mode
- **WHEN** the last workflow step reports `done` and `ship = "mr"`
- **THEN** the run's state becomes `shipping` and nothing is pushed until the change is archived in the branch

### Requirement: Archive in the merge request
Before its first push, a shipping run SHALL archive the change on its branch with `openspec archive <change> --yes`, validate all specs strictly, and commit the result, so the archive merges with the work. `[shipping] archive = false` SHALL skip this. A failed archive or validation SHALL block the run with the command's output, and push nothing.

#### Scenario: Archive commit
- **WHEN** a change's last step is done in `mr` mode with the default settings
- **THEN** the branch's newest commit moves the change to `openspec/changes/archive/` and updates `openspec/specs/`, before the branch is pushed

#### Scenario: Archive fails
- **WHEN** `openspec archive` fails because a delta does not apply
- **THEN** the run is `blocked` with the output and the branch is not pushed

### Requirement: Opening the merge request
After its first push, a shipping run SHALL open a merge request (a pull request on GitHub) from the change's branch to the base branch, ready for review, not draft. Its title SHALL be the board card's title. Its body SHALL hold the proposal's Why and What Changes, the change id, and a hidden claim marker (see ship-claim). `[shipping] labels` SHALL be added if set.

#### Scenario: Pull request on GitHub
- **WHEN** change `add-login` with no `title` is pushed to a GitHub repository
- **THEN** a pull request titled `add login` is open from branch `add-login` to the base, not draft, with the change id in its body

#### Scenario: Body too long
- **WHEN** the proposal's sections are longer than the forge allows
- **THEN** the body is cut, ends with a link to the proposal on the branch, and the merge request still opens

### Requirement: Stopping at green in mr mode
With `ship = "mr"`, a shipping run SHALL become `ready` once CI on the head commit is `green` or `none` and no review thread waits for a fix (see ship-feedback). It SHALL NOT merge. The merge request link SHALL stay on the run.

#### Scenario: Green in mr mode
- **WHEN** CI on the head commit passes in `mr` mode and no review thread is waiting
- **THEN** the run is `ready` with the merge request link, and the merge request is still open

### Requirement: Merging in merge mode
With `ship = "merge"`, a shipping run SHALL merge when every check on the head commit has passed, no review thread waits for a fix, and the forge reports the merge request mergeable. The merge SHALL be pinned to the head commit seen green, so the forge refuses it if the head moved. The run SHALL then be `merged`. Model: `formal/ship.tla`, `MergedGreen`.

#### Scenario: Merge when green
- **WHEN** checks on head `abc123` pass, no thread waits and the forge reports the merge request mergeable
- **THEN** Tuckerbox merges with the head pinned to `abc123` and the run is `merged`

#### Scenario: Head moved before the merge
- **WHEN** someone pushes to the branch after Tuckerbox saw `abc123` green and before it merges
- **THEN** the forge refuses the pinned merge, nothing is merged, and the run is `blocked` with reason `branch changed on the forge`

#### Scenario: Protection not met
- **WHEN** checks pass but the forge says a required approval is missing
- **THEN** Tuckerbox does not merge, the run stays `shipping` with reason `waiting for approval`, and polling continues

### Requirement: Merge method
`[shipping] merge_method` SHALL be `merge`, `squash` or `rebase`, `merge` by default. It SHALL only be used in `merge` mode. A method the forge project does not allow SHALL block the run with the forge's message.

#### Scenario: Squash
- **WHEN** `merge_method = "squash"` and the merge request is ready to merge
- **THEN** Tuckerbox asks the forge for a squash merge

### Requirement: Branch behind the base
When the forge reports the branch must be updated before merging, a run in `merge` mode SHALL merge the base branch into the change's branch with `git merge`, push it, and poll CI again. It SHALL NOT rebase. A merge conflict SHALL block the run with the conflicting files.

#### Scenario: Conflict with the base
- **WHEN** the base moved and merging it into the change's branch conflicts in `go.mod`
- **THEN** the merge is aborted, nothing is pushed, and the run is `blocked` naming `go.mod`

### Requirement: Cleaning up after a merge
After a merge, Tuckerbox SHALL delete the remote change branch if its tip is still the merged commit, and leave it otherwise. It SHALL keep the local worktree and branch.

#### Scenario: Branch removed
- **WHEN** Tuckerbox merged the change and the remote branch tip is the merged commit
- **THEN** the remote branch is deleted and the worktree is kept

### Requirement: Merge request closed
When the merge request is closed without merging, or merged by someone else, while a run is shipping, the run SHALL stop polling. A closed one SHALL leave it `blocked` with reason `merge request closed`. One merged by someone else SHALL make it `merged`.

#### Scenario: Closed by a person
- **WHEN** a person closes the merge request while CI runs
- **THEN** the run is `blocked` with reason `merge request closed` and pushes nothing more

### Requirement: Push safety
Tuckerbox SHALL push only the change's branch, by an explicit refspec, and SHALL refuse to push when that branch is the base branch or the forge's default branch. Every push SHALL use `--force-with-lease=<branch>:<commit>` with the commit it last pushed, or no branch for the first push. It SHALL never push with plain `--force`.

#### Scenario: Branch pattern names the base
- **WHEN** the branch pattern makes the change's branch `main` and the base is `main`
- **THEN** the run is `blocked` before any push and says it will not push the base branch

### Requirement: Merge safety
Tuckerbox SHALL NOT merge while any check on the head commit is failing or pending, while a check the forge marks required is missing, or when no check reported at all. It SHALL NOT use an option that skips the forge's protection, even with an admin token.

#### Scenario: No CI at all
- **WHEN** CI on the head commit is `none` in `merge` mode
- **THEN** the run is `blocked` with reason `no checks reported` and does not merge

### Requirement: Dependencies when shipping
With `ship = "mr"` or `"merge"`, a change's dependency SHALL count as met only once it is archived on the base branch, because the dependent's worktree is made from the base. Being `ready` on this machine SHALL NOT be enough.

#### Scenario: Dependency waits for merge
- **WHEN** `ship = "mr"`, change B depends on A, and A is `ready` with its merge request open
- **THEN** B stays `queued (waiting on add-a (ready))` until A is merged and archived on the base branch

### Requirement: Starting a shipping run
In `mr` or `merge` mode, `tuckerbox run` SHALL check before starting that the forge is known and a token is found, and refuse otherwise. If an open merge request for the change's branch carries another machine's claim, it SHALL refuse and link it. If the forge can't be reached, it SHALL warn and start.

#### Scenario: Claimed elsewhere
- **WHEN** an open merge request for `add-login` carries machine `studio`'s claim and the user runs `tuckerbox run add-login` on `mini`
- **THEN** it refuses, names `studio` and links the merge request

#### Scenario: No token
- **WHEN** `ship = "merge"` and no token is found for the forge's host
- **THEN** `tuckerbox run` refuses and names the keys and variables that would supply one
