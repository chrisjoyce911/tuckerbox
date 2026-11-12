# Spec Delta

## MODIFIED Requirements

### Requirement: Opening and hiding the board
The toggle key, the drawer, the top bar's Tuckerbox button and `mod.open tuckerbox-board` SHALL all open the board window when it is closed and close it when it is open. The board SHALL NOT offer `tuckerbox init`; the top bar's Set up Tuckerbox is its only entry point in Herdr (see herdr-topbar).

#### Scenario: Toggle twice
- **WHEN** the person presses the toggle key in a Tuckerbox repository's space, then presses it again
- **THEN** the board window opens, then closes

### Requirement: Entry points need a Tuckerbox config
Every entry point SHALL appear only when the shown space has a Tuckerbox repository (see Which repository), except the top bar's Set up Tuckerbox, which appears in a git repository without a config. Without one:
- the drawer draws nothing
- there is no Dock button
- the toggle opens no board: the window closes at once and a short toast says `No Tuckerbox config in this space`

A board window that has shown a board and then follows the person to a space without one SHALL say `No Tuckerbox config in this space` in place of the columns.

#### Scenario: Toggle in a space without a config
- **WHEN** the person presses the toggle key in a space whose terminals are in no git repository with `.tuckerbox/config.toml`
- **THEN** no board window stays open and a toast says `No Tuckerbox config in this space`

#### Scenario: Only set up without a config
- **WHEN** the shown space is in a git repository without `.tuckerbox/config.toml`
- **THEN** the drawer draws nothing, the toggle opens no board, and the top bar shows only Set up Tuckerbox
