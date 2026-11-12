# Spec Delta

## Purpose

Defines Tuckerbox's place in the Herdr app's top bar: how it is installed beside the tabs, what it shows in each kind of space, its menu, the board in the side panel, autopilot from the bar, and setting up a repository that has no Tuckerbox config yet.

## ADDED Requirements

### Requirement: Installing the top bar
`tuckerbox herdr install --topbar` SHALL add a block `tuckerbox-bar` (the board mod, props `{view: "bar", block: "tuckerbox-bar"}`) right after the block `tabs` in its row in the shown layout. A shipped layout SHALL be cloned first as for `--drawer`, sharing its clone. Install SHALL use the same lock, journal and record as `--drawer`.

#### Scenario: Top bar on Herdr's own layout
- **WHEN** Herdr shows its shipped `main` layout and the person runs `tuckerbox herdr install --topbar`
- **THEN** `layouts/main.json` is unchanged, `main-tuckerbox` is shown, and its row with `tabs` has `tuckerbox-bar` right after `tabs`

#### Scenario: No tabs block
- **WHEN** the shown layout has no block `tabs` in a row
- **THEN** install changes no layout, prints the block to add by hand, and exits non-zero after installing the mod

### Requirement: A board in the side panel
With `--topbar`, when the layout has a stack `panel-body`, install SHALL also add a block `tuckerbox-panel` (the board mod, props `{view: "panel"}`) as that stack's last child, and record it. Without that stack it SHALL skip this, say so, and still add the bar.

#### Scenario: Panel beside Files and Changes
- **WHEN** the layout's `panel-body` stack holds `files` and `changes`
- **THEN** after install it holds `files`, `changes` and `tuckerbox-panel`, in that order

### Requirement: Uninstalling the top bar
`tuckerbox herdr uninstall` SHALL remove `tuckerbox-bar` and `tuckerbox-panel` as its record says, with the rules for the drawer: a layout unchanged since install gets its earlier bytes back, a changed one loses only these blocks, and an unchanged clone is removed.

#### Scenario: Install then uninstall
- **WHEN** the person installs with `--topbar --drawer`, changes nothing, and uninstalls
- **THEN** every file in the Herdr home is byte for byte as before install

### Requirement: The bar sizes its block
The bar SHALL set its block's width with `layout.resize {id: props.block, px}` from its content's measured width divided by the app's zoom, and only when that width changed. When the shown space has no git repository, it SHALL draw nothing and set its block to 0 px.

#### Scenario: Space without a repository
- **WHEN** the person switches to a space whose terminals are in no git repository
- **THEN** the bar draws nothing and its block is 0 px wide, so the tabs reach the bar's end

#### Scenario: Zoomed app
- **WHEN** the app's zoom is 1.25 and the bar's content measures 150 px
- **THEN** the bar resizes its block to 120 px

### Requirement: The bar in a Tuckerbox repository
In a space whose repository has `.tuckerbox/config.toml`, the bar SHALL show a Tuckerbox button and a ▾ menu button, with badges for running changes and changes that need the person, read as the drawer reads them, with the same stale-read rules. The button SHALL open or close the board window.

#### Scenario: Badges
- **WHEN** the shown space's repository has two running changes and one blocked
- **THEN** the bar shows `2` running and a `1` needs-you badge

#### Scenario: Button toggles the board
- **WHEN** the person clicks the Tuckerbox button twice
- **THEN** the board window opens, then closes

### Requirement: The bar's menu
The ▾ button SHALL open a popup overlay just under the bar, placed for the app's zoom, with: Board, Board in side panel (when `tuckerbox-panel` is in the layout), Settings, and Autopilot. Choosing an item SHALL close the menu. A second click on ▾ SHALL close it.

#### Scenario: Open Settings from the bar
- **WHEN** the person chooses Settings from the menu
- **THEN** the board window opens showing the Settings view for that repository

### Requirement: Board in the side panel
Board in side panel SHALL run `layout.select {stack: "panel-body", child: "tuckerbox-panel"}` and `layout.show {id: "panel"}`, as Herdr's Files and Changes buttons do, and SHALL hide the panel when the Tuckerbox board is already showing there. The panel board SHALL follow the shown space.

#### Scenario: Board beside the terminals
- **WHEN** the person chooses Board in side panel while the panel is closed
- **THEN** the side panel opens showing the board of the shown space's repository

### Requirement: Autopilot from the bar
When approval is required, the menu SHALL show whether autopilot runs for the repository. Start SHALL run `tuckerbox autopilot` in a herdr-core terminal named `tb-ap-` and 12 hex digits of the repository's hash, on the space's machine. Stop SHALL send that terminal Ctrl-C. An autopilot started elsewhere SHALL show as running, with no Stop.

#### Scenario: Start and stop
- **WHEN** autopilot isn't running and the person chooses Start autopilot, then later Stop autopilot
- **THEN** a terminal named for the repository runs `tuckerbox autopilot`, the menu says it runs, and after Stop it exits as on Ctrl-C and the menu says it doesn't

#### Scenario: Started in a shell
- **WHEN** the owner runs `tuckerbox autopilot` in their own terminal
- **THEN** the menu says autopilot runs with its process id, and offers no Stop

#### Scenario: Approval off
- **WHEN** the repository sets `require_approval = false`
- **THEN** the menu has no Autopilot item

### Requirement: Setting up Tuckerbox
In a space whose git repository has no `.tuckerbox/config.toml`, the bar SHALL show only Set up Tuckerbox. It SHALL open a terminal pane on the space's machine running `tuckerbox init` at the repository's top level. When that exits 0 and the config exists, the bar SHALL open the board.

#### Scenario: New project
- **WHEN** the person clicks Set up Tuckerbox in a space whose repository has no config
- **THEN** a pane runs `tuckerbox init` there, and once it succeeds the bar shows the Tuckerbox button and the board window opens

#### Scenario: Init fails
- **WHEN** `tuckerbox init` exits non-zero
- **THEN** its pane stays open with the error and the bar still shows Set up Tuckerbox

### Requirement: The bar writes no files
The bar and its menu SHALL change files only through `tuckerbox` commands and Herdr actions, as every board action does.

#### Scenario: Set up writes through init
- **WHEN** the person clicks Set up Tuckerbox
- **THEN** only `tuckerbox init` writes `.tuckerbox/config.toml`
