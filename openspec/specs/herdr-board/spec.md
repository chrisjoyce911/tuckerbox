# herdr-board Specification

## Purpose
Defines the Tuckerbox board inside the Herdr app: what it shows, what its actions do, and how it brings several machines together.

## Requirements

### Requirement: Installing the mod
`tuckerbox herdr install` SHALL copy the board mod into the Herdr app's mods folder, run `herdr-app check` on it, and report the result. If Herdr is not installed, it SHALL say so and change nothing. Re-running it SHALL update the mod in place. Herdr's home is `$HERDR_APP_HOME`, else `~/.config/herdr-app`.

#### Scenario: Herdr missing
- **WHEN** a machine has no Herdr app config folder
- **THEN** `tuckerbox herdr install` exits non-zero, says Herdr wasn't found and writes nothing

### Requirement: Install record
Install SHALL write a record of every change it made in the Herdr home (`.tuckerbox-install.json`):
- the mod folders it installed
- the key binding it added, with `keys.json` as it was before
- the drawer it added, with the layout file as it was before, and the layout it cloned, if any

A step that fails SHALL leave its files unchanged and SHALL NOT be recorded.

#### Scenario: Drawer step fails
- **WHEN** `tuckerbox herdr install --drawer` must clone a shipped layout and Herdr isn't running
- **THEN** it says Herdr must be running, no layout file changes, and the record has no drawer

### Requirement: Toggle key
`tuckerbox herdr install --keys` SHALL bind the key the mod ships in its own `keys.json` to `mod.open:tuckerbox-board` in the `global` section of the person's `keys.json`, keeping the file's other entries and their order. The shipped key is `ctrl-shift-k`, spelled `cmd-shift-k` on macOS as Herdr's own shortcuts are; `--key <key>` SHALL choose another. Without `--keys`, install SHALL NOT touch `keys.json`.

#### Scenario: Key bound
- **WHEN** the person runs `tuckerbox herdr install --keys` on macOS and `cmd-shift-k` is free
- **THEN** `keys.json` gains `"cmd-shift-k": "mod.open:tuckerbox-board"` in `global` and nothing else changes

### Requirement: Toggle key never replaces a binding
When the key is already bound, with modifiers in any order and any case, in any section other than a `mod:` section, install SHALL leave `keys.json` unchanged. It SHALL name the key, its section and its action, suggest `--key`, still install the mod and the drawer, and exit non-zero. When the same key already opens the board, it SHALL say so and change nothing. Without a `keys.json`, it SHALL ask the person to open Herdr once and SHALL NOT write one.

#### Scenario: Key clash
- **WHEN** `cmd-shift-k` is already bound to `app.palette` in `global`, or to anything in `terminal` or a `layout:` section
- **THEN** install leaves `keys.json` byte for byte as it was, reports `cmd-shift-k is already bound to app.palette` and the section, and exits non-zero after installing the mod

#### Scenario: Edited keys file
- **WHEN** install adds the key to a `keys.json` Herdr still updates by itself
- **THEN** it says Herdr no longer adds new default shortcuts to it until `tuckerbox herdr uninstall` puts it back

### Requirement: Opening and hiding the board
The toggle key, the drawer and `mod.open tuckerbox-board` SHALL all open the board window when it is closed and close it when it is open. The board SHALL NOT offer `tuckerbox init`; that stays a CLI command.

#### Scenario: Toggle twice
- **WHEN** the person presses the toggle key in a Tuckerbox repository's space, then presses it again
- **THEN** the board window opens, then closes

### Requirement: Entry points need a Tuckerbox config
Every entry point SHALL appear only when the shown space has a Tuckerbox repository (see Which repository). Without one:
- the drawer draws nothing
- there is no Dock button
- the toggle opens no board: the window closes at once and a short toast says `No Tuckerbox config in this space`

A board window that has shown a board and then follows the person to a space without one SHALL say `No Tuckerbox config in this space` in place of the columns.

#### Scenario: Toggle in a space without a config
- **WHEN** the person presses the toggle key in a space whose terminals are in no git repository with `.tuckerbox/config.toml`
- **THEN** no board window stays open and a toast says `No Tuckerbox config in this space`

### Requirement: Which repository
The board SHALL take the first of these inside a Tuckerbox repository, on the space's machine:
- its `folder` prop (a docked board), with its `machine` prop as the machine
- the focused pane's folder, when that pane is in the board's space
- every other pane of that space, the shown tab's first, then the space's folder

A folder is inside a Tuckerbox repository when its git checkout's top level has `.tuckerbox/config.toml`. The board reads `tuckerbox board --json` at that top level.

#### Scenario: Focused pane outside the repository
- **WHEN** the focused pane is in `/tmp` and another pane of the same space is in a checkout with `.tuckerbox/config.toml`
- **THEN** the board shows that checkout's board

#### Scenario: Git repository without a config
- **WHEN** every pane of the shown space is in a git repository without `.tuckerbox/config.toml`
- **THEN** the board shows `No Tuckerbox config in this space` and offers no command

### Requirement: Boards follow their own space
A board window SHALL follow the shown space. Focus in another space SHALL NOT move a board to that space's repository or machine.

#### Scenario: Focus elsewhere
- **WHEN** a board pane belongs to space A and the person focuses a terminal of space B on another machine
- **THEN** the board keeps reading A's repository on A's machine

### Requirement: Docking the board
The board window's header SHALL offer Dock while it shows a board. Dock SHALL open the board as a mod pane with `pane.split {direction: "right", mod: "tuckerbox-board", props: {folder, machine}}`, then close the window. `folder` is the repository's top level and `machine` its Herdr machine (null for this computer). It splits the focused pane when that is a terminal in the board's space, else the shown tab's first terminal, else it opens a tab with the board as its only pane.

#### Scenario: Dock beside the terminals
- **WHEN** the person clicks Dock on the board window of `/work/shop` on this computer with a shell focused
- **THEN** a board pane opens to the right of that shell with props `{folder: "/work/shop", machine: null}` and the window closes

### Requirement: Undocking the board
A docked board SHALL read the repository in its `folder` prop. Its header SHALL offer Undock, which opens the board window again and closes the pane, and Close, which only closes the pane.

#### Scenario: Undock
- **WHEN** the person clicks Undock on a docked board
- **THEN** the board window opens and the pane closes

### Requirement: Adding the drawer
`tuckerbox herdr install --drawer` SHALL add a floating peek drawer (`id` `tuckerbox-drawer`, the board mod with props `{view: "drawer"}`, right edge, low down) to the root `floating` list of the layout Herdr shows. A personal layout SHALL be edited in place, keeping its other entries in order. A shipped layout is read-only: install SHALL clone it first with `herdr-app do layout.clone {name, as: "<name>-tuckerbox"}`, which also shows the clone, and add the drawer there.

#### Scenario: Drawer on Herdr's own layout
- **WHEN** Herdr shows its shipped `main` layout and the person runs `tuckerbox herdr install --drawer`
- **THEN** `layouts/main.json` is unchanged, `main-tuckerbox` is shown and has the drawer

### Requirement: What the drawer shows
The drawer SHALL draw nothing unless the shown space has a Tuckerbox repository. Then it SHALL show the repository's name and the counts of running changes and of changes needing the person, read with `tuckerbox board --json` every 30 s and at once when the shown space changes. A click SHALL open or close the board window.

#### Scenario: Drawer in a space without a config
- **WHEN** the shown space has no Tuckerbox repository
- **THEN** the drawer draws nothing

#### Scenario: Drawer counts
- **WHEN** the shown space's repository has two running changes and one blocked
- **THEN** the drawer shows the repository's name, `2 running` and `1 needs you`

### Requirement: No new-menu entry
Install SHALL NOT change Herdr's + (new) menu. Herdr documents a choice of terminal right-click menu (`terminal_menu`) and of toast mod, but no way to choose a personal new-menu, and its shipped mods are read-only.

#### Scenario: No menu flag
- **WHEN** the person runs `tuckerbox herdr install --menu`
- **THEN** it fails with a usage error and changes nothing

### Requirement: Uninstalling
`tuckerbox herdr uninstall` SHALL reverse what the install record says, and nothing else: the drawer, the key, then the mod folders, then the record. A file still exactly as install left it SHALL get its earlier bytes back. A file the person changed since SHALL lose only Tuckerbox's entry and keep the rest. Without a record it SHALL remove only the board mod folder. With neither, it SHALL say the board isn't installed and change nothing.

#### Scenario: Install then uninstall
- **WHEN** the person runs `tuckerbox herdr install --keys --drawer` and then `tuckerbox herdr uninstall` without changing anything between
- **THEN** every file in the Herdr home is byte for byte as before install, and the mod folder and the record are gone

#### Scenario: Changed since install
- **WHEN** the person added a key to `keys.json` after install
- **THEN** uninstall removes only the toggle key and keeps theirs

### Requirement: Uninstalling a cloned layout
A layout install cloned SHALL be removed when it is unchanged since install. If the clone is shown, uninstall SHALL first switch Herdr back to the layout it was cloned from, which needs Herdr running. A clone the person changed SHALL be kept, without the drawer.

#### Scenario: Clone shown at uninstall
- **WHEN** `main-tuckerbox` is shown and unchanged since install
- **THEN** uninstall switches Herdr to `main` and removes `layouts/main-tuckerbox.json`

### Requirement: Board for a space's repository
The board SHALL show the repository of the Herdr space it is opened from (see Which repository), using `tuckerbox board --json`. It SHALL refresh when run state changes, and at most every 5 seconds while shown. The header SHALL show the repository, the count of running changes, and the count of changes needing attention.

#### Scenario: Space without a Tuckerbox config
- **WHEN** the board is opened from a space with no folder in a git repository that has `.tuckerbox/config.toml`
- **THEN** it shows no board, no Dock button and no `tuckerbox init` offer: a window closes with the toast `No Tuckerbox config in this space`, a docked pane says it in place of the columns

### Requirement: Runs from every machine
The board SHALL ask each connected Herdr machine for the same repository's board, matched by remote URL, and merge the cards by change id. Each card SHALL show the machine its run is on. An unreachable machine SHALL be named in the header and SHALL NOT hide the other machines' cards.

#### Scenario: Two machines
- **WHEN** change A runs on machine `studio` and change B on machine `mini`, both in the same repository
- **THEN** one board shows both cards, each labelled with its machine

### Requirement: Actions go through Tuckerbox
Every board action SHALL run a `tuckerbox` command on the card's machine, and the board SHALL show the command's error if it fails. The board SHALL NOT write run state, and SHALL NOT write any file itself. OpenSpec files change only through the person's editor or a `tuckerbox` command the board runs. Start SHALL be offered only on Approved cards, or on Proposed cards when approval is not required.

#### Scenario: Start from the board
- **WHEN** the user clicks Start on an Approved card
- **THEN** the board runs `tuckerbox run <change>` in the repository and the card moves to Queued or the first step's column when the next board read shows it there

#### Scenario: Start not offered before approval
- **WHEN** approval is required and a card is in Proposed
- **THEN** the card offers Approve but not Start

#### Scenario: Action fails
- **WHEN** Start fails because the base branch has no approval for the change yet
- **THEN** the board shows the command's message and the card stays in Approved

### Requirement: Run actions
The board SHALL offer these actions for the card's state:
- Stop, Resume, and Re-run step
- Follow log: opens a Herdr tab running `tuckerbox logs <change> <step> --follow`
- Open proposal: shows the proposal in a pane beside the board
- Open worktree: opens the change's worktree as a Herdr space

#### Scenario: Follow a running step
- **WHEN** the user clicks Follow log on a card running its build step on machine `mini`
- **THEN** a Herdr tab opens on `mini` running `tuckerbox logs <change> build --follow`

### Requirement: Editing from the board
Card detail SHALL offer Edit for the proposal, design, model, tasks and each spec delta. Edit SHALL open a Herdr pane on the card's machine running `tuckerbox edit <change> <file>`. When that pane closes, the board SHALL run `tuckerbox validate <change> --json` and show the result on the card, kept in memory only. Edit SHALL NOT be offered while the change has a queued or running run.

#### Scenario: Edit leaves the change invalid
- **WHEN** the user edits a spec delta from the board, breaks a requirement and closes the editor
- **THEN** the card shows the validation error

#### Scenario: Edit during a run
- **WHEN** a card is running its build step
- **THEN** its detail offers no Edit, and if a run starts while the edit pane opens, the pane shows the command's refusal

### Requirement: Tasks and approval from the board
Card detail SHALL let the person tick and untick each task by running `tuckerbox tasks <change> tick|untick <n.m>`, except while the change has a queued or running run. It SHALL show Approve when the approval state is `none` or `stale`, and Unapprove when it is `approved`, running `tuckerbox approve` or `tuckerbox unapprove`, except while the change has a queued or running run (both take the run lock and would refuse). It SHALL show the command's note that nothing was committed.

#### Scenario: Tick from the board
- **WHEN** the user ticks task 2.1 on a stopped card
- **THEN** the board runs `tuckerbox tasks <change> tick 2.1` and the next board read shows 2.1 ticked

#### Scenario: Approve from the board
- **WHEN** the user clicks Approve on a valid Proposed card
- **THEN** the board runs `tuckerbox approve <change>`, shows that the approval is not committed, and the card moves to Approved on the next board read

#### Scenario: Approve an invalid change
- **WHEN** the user clicks Approve and the change fails strict validation
- **THEN** the board shows the validation errors and the card stays in Proposed

### Requirement: Card detail view
Opening a card SHALL show:
- the proposal, rendered as markdown
- the task list with progress and tick boxes
- the approval state, who approved and when, and whether it is committed
- the last validation result from an edit, if any
- dependencies with their columns
- each round with its result and findings
- the history, newest first, times in local time
- the actions available for the card's state

#### Scenario: Rejected change
- **WHEN** the user opens a card whose review rejected round 1
- **THEN** the detail shows round 1's findings under round 1, and the history shows the hand-off back to the build step

### Requirement: Run group from the board
Card detail SHALL offer **Run group…** wherever it offers Start, and on Ready cards whose change still has unticked tasks. It SHALL list only groups with unticked tasks. Choosing one SHALL run `tuckerbox run <change> --tasks <group>` on the card's machine, and the board SHALL show the command's error if it fails.

#### Scenario: Run the next group from a Ready card
- **WHEN** a Ready card's run had scope `1` and the user picks group 2 under Run group…
- **THEN** the board runs `tuckerbox run <change> --tasks 2`, and the card leaves Ready on the next board read

#### Scenario: Nothing left to run
- **WHEN** every task of a Ready card's change is ticked
- **THEN** its detail offers no Run group…

#### Scenario: Run group refused
- **WHEN** the user picks a group and the run gate refuses because the approval is stale
- **THEN** the board shows the command's message and the card stays where it was
