# Spec Delta

## Purpose

Defines which repository and machine the Herdr board mod reads, how it discards reads that finish after the space changed, how its columns scroll when they don't fit, how Done cards show approval, and where change titles come from.

## ADDED Requirements

### Requirement: The repository's main checkout
When the board resolves a folder to a repository, it SHALL use the main worktree of the folder's repository: the first entry of `git worktree list`. It SHALL look for `.tuckerbox/config.toml` and read `tuckerbox board --json` there. A folder in a linked worktree, such as a change's worktree, SHALL resolve to the main worktree. If the main worktree is bare, it SHALL use the folder's own top level.

#### Scenario: Pane in a change's worktree
- **WHEN** the only pane of the shown space is in the worktree of change `add-login`, a linked worktree of the checkout `/work/shop`
- **THEN** the board reads `/work/shop` and shows the whole repository's board

#### Scenario: Docked on a worktree folder
- **WHEN** a docked board's `folder` prop is a linked worktree of `/work/shop`
- **THEN** it reads `/work/shop`

#### Scenario: Plain checkout
- **WHEN** the pane is in `/work/shop`, which has no linked worktrees
- **THEN** the board reads `/work/shop`, as before

### Requirement: Explicit machine pin
A board's `machine` prop SHALL count whenever the key is present: a name means that Herdr machine and `null` means this computer, even when the board's space is on another machine. Only an absent `machine` prop SHALL fall back to the space's machine.

#### Scenario: Pinned to this computer in a remote space
- **WHEN** a board pane with props `{folder: "/local/shop", machine: null}` is in a space on machine `vps`
- **THEN** the board reads `/local/shop` on this computer, not on `vps`

#### Scenario: No machine prop
- **WHEN** a board pane with props `{folder: "/srv/shop"}` is in a space on machine `vps`
- **THEN** the board reads `/srv/shop` on `vps`

### Requirement: Stale reads are dropped
The board window and the drawer SHALL tie each read to the folders and machine it started from. When those change, the view SHALL stop showing the old result at once and start a read for the new ones, even while another read runs. A read that finishes for folders or a machine no longer current SHALL be discarded. Model: `formal/boardview.tla`, `ShownIsCurrent` and `Settles`.

#### Scenario: Drawer moves to a space without a config
- **WHEN** the drawer is reading repository A's board and the person switches to space B, which has no Tuckerbox config
- **THEN** A's counts are never shown in B, and the drawer draws nothing in B once the current read finishes

#### Scenario: Back to the first space
- **WHEN** the drawer is reading A, the person switches to B and back to A, and B's read finishes after A's
- **THEN** the drawer shows A's counts and B's result is discarded

#### Scenario: Board window moves to another repository
- **WHEN** the board window is reading repository A and the shown space changes to repository C
- **THEN** A's result is discarded, C's read starts at once, and A's cards and actions are not shown again

### Requirement: No approval badge on Done cards
Cards in Done SHALL show no approval badge on the board or in card detail, whatever the card data's approval state.

#### Scenario: Archived change
- **WHEN** the board shows an archived change in Done
- **THEN** its card shows no approval badge, neither `not approved` nor `approved`

#### Scenario: Active change without approval
- **WHEN** a Proposed card has approval state `none`
- **THEN** it still shows the `not approved` badge

### Requirement: Columns scroll sideways
In the board window and in a docked board pane, every column SHALL keep a fixed width of 236 px. When the columns don't fit, the column row SHALL scroll horizontally with a visible scrollbar, by trackpad swipe and by dragging the scrollbar. It SHALL NOT squeeze, wrap or hide columns. The header SHALL stay in place.

#### Scenario: Narrow docked pane
- **WHEN** the board is docked in a pane 500 px wide and has five columns
- **THEN** the first two columns show at full width, a horizontal scrollbar shows, and scrolling right reaches Ready and Done

#### Scenario: Wide window
- **WHEN** every column fits in the board window
- **THEN** no horizontal scrollbar shows and nothing scrolls sideways

### Requirement: Shift and the mouse wheel step through columns
Over a column row that doesn't fit, a wheel movement with shift held and no sideways part SHALL reveal the next column to the right (wheel down) or left (wheel up), one column per 236 px of movement, and SHALL NOT scroll a column's list. A wheel movement that already has a sideways part SHALL be left to Herdr's own scrolling.

#### Scenario: Shift and the mouse wheel
- **WHEN** the board is docked 500 px wide showing Proposed and Approved, and the person turns the wheel down one column's worth with shift held
- **THEN** In progress comes into view and no column's list moves

#### Scenario: Sideways wheel left alone
- **WHEN** the platform delivers shift with the wheel as a sideways movement
- **THEN** the board leaves it to Herdr's scrolling and does not step a column as well

### Requirement: Titles of this repository's changes
A card's title SHALL come from `title` in the change's `tuckerbox.toml`, else the change id with dashes as spaces. The proposal's first heading SHALL NOT be used. Every active change in this repository SHALL have a `tuckerbox.toml` with a non-empty `title`, and a test SHALL fail when one does not.

#### Scenario: Herdr board change
- **WHEN** the board shows change `add-herdr-board`
- **THEN** its title is `Herdr board`

#### Scenario: Change added without a title
- **WHEN** a new change folder in this repository has no `title` in its `tuckerbox.toml`
- **THEN** `go test ./...` fails and names the change
