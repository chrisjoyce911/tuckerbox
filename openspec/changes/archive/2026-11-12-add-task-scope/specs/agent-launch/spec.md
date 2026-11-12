# Spec Delta

## MODIFIED Requirements

### Requirement: Step brief
Before starting a step, Tuckerbox SHALL write a brief file the agent reads first. The brief contains:
- the step's role prompt (the repository's, or the built-in one for that role)
- the change id and the paths of its proposal, design, tasks and spec deltas
- the result format, and that the agent ends with it as its final message
- findings from earlier rounds, when the step is re-run after a rejection
- the run's task scope, if it has one (see task-scope)

#### Scenario: Build after rejection
- **WHEN** the build step starts after the review step rejected round 1
- **THEN** its brief includes round 1's findings

#### Scenario: Brief for a scope
- **WHEN** a step starts for a run with scope `2` covering tasks 2.1 and 2.2
- **THEN** its brief lists 2.1 and 2.2 with their text, and says that tasks outside the scope are not part of this run: not to be done, ticked, or rejected for being undone
