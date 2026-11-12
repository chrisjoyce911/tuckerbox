# Spec Delta

## ADDED Requirements

### Requirement: Autopilot in the board document
`tuckerbox board --json` SHALL include `autopilot`: `running` (true while autopilot's lock for the repository is held on this machine), and while running its process id, machine, start time, and the herdr-core terminal's name when it runs in one. It SHALL read this from autopilot's lock and marker, starting nothing.

#### Scenario: Autopilot running
- **WHEN** autopilot runs for the repository in terminal `tb-ap-3f2a9c1d0b4e`
- **THEN** the board document has `autopilot.running` true, its process id, this machine, its start time and that terminal

#### Scenario: Marker left by a crash
- **WHEN** an autopilot died and left its marker, and no process holds its lock
- **THEN** `autopilot.running` is false
