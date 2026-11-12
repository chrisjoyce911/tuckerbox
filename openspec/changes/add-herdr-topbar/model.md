# Model

## Skipped

No new shared state without an atomic primitive: install runs under the install lock and journal from `fix-board-safety`, and two autopilots for one repository are prevented by herdr-core's unique terminal name and autopilot's own lock (`add-autopilot`), both already atomic.
