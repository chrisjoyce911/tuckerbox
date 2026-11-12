# Spec Delta

## ADDED Requirements

### Requirement: Drag to reorder
In the board mod, the owner SHALL be able to drag a card within a sorted column, not into another column. A drop SHALL run `tuckerbox priority <change> <n>` on the board's own machine, with `n` the position that puts the card where it was dropped. The board SHALL show the command's error, and the order SHALL change only when the next board read shows it.

#### Scenario: Drag above a card
- **WHEN** the owner drags D, at position 5, above B, at position 2, in the Approved column
- **THEN** the board runs `tuckerbox priority D 2`, and D is listed above B on the next read

#### Scenario: Drag down
- **WHEN** the owner drags A, at position 1, below C, at position 3
- **THEN** the board runs `tuckerbox priority A 3`
