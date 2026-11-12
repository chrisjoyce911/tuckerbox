# Spec Delta

## ADDED Requirements

### Requirement: Priority on the board
Each active card SHALL include its `priority` value, or none, and its `position` in priority order. The Proposed, Approved and In progress columns, and in the steps layout Queued and each step column, SHALL list cards in priority order. Ready and Done SHALL keep their order. Positions SHALL come from the board's own machine.

#### Scenario: Board order
- **WHEN** two Approved cards, A with priority 2000 and B with 1000, are read
- **THEN** B is listed before A, with positions in priority order
