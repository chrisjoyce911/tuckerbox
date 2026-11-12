# blocked-answers Specification

## Purpose
Defines how a person answers a run that blocked with a question, from the board or the command line, and how that answer reaches the agent when the run resumes, or at once when a terminal agent is waiting.

## Requirements

### Requirement: The question of a blocked run
A run blocked by its step's `blocked` result SHALL show that result's summary and findings as its question, on the card, in card detail, and in `tuckerbox status`, with the step and round that asked it.

#### Scenario: Builder blocked on a decision
- **WHEN** `fix-board-safety`'s build step reports `blocked` with summary "Live Herdr check needed; keep or drop install rollback?"
- **THEN** its card shows that summary as the question, with step `build` and the round

### Requirement: Answering a blocked run
`tuckerbox answer <change> <text>` (or `--file <path>`, or `-` for standard input) SHALL record an answer for a `blocked` run: its text, who wrote it (git `user.name`), when, and the step and round it answers, in run state and as an `answered` history event. It SHALL take the change's run lock to write. It SHALL refuse a run that isn't blocked, and text over 16 KiB.

#### Scenario: Answer from the command line
- **WHEN** the owner runs `tuckerbox answer fix-board-safety "Herdr check done by hand: drawer works. Keep rollback."` on the blocked run
- **THEN** run state holds the answer with the owner's name, the time and step `build`, history has an `answered` event, and the run stays `blocked`

#### Scenario: Run not blocked
- **WHEN** the user runs `tuckerbox answer add-login "..."` while `add-login` is running headless
- **THEN** it exits non-zero, says the run isn't blocked, and records nothing

### Requirement: Answers in the next brief
When a blocked run with answers resumes, the brief of the step it resumes SHALL include the question (the blocked result's summary and findings) and every answer given since, each with who wrote it and when, under a heading saying these are the person's decisions. The answers SHALL stay in that step's briefs until it reports a result, then count as used.

#### Scenario: Resume with the decision
- **WHEN** `fix-board-safety` has the owner's answer and the owner runs `tuckerbox resume fix-board-safety`
- **THEN** the build step's new brief holds the question and the answer, and the agent continues from that decision

#### Scenario: Two answers
- **WHEN** the owner answers twice before resuming
- **THEN** the brief holds both answers, oldest first

### Requirement: Answer and resume
`tuckerbox answer <change> --resume <text>` SHALL record the answer and then resume the run as `tuckerbox resume` does, with the same gates and refusals. If the resume is refused, the answer SHALL still be recorded, and the output SHALL say so.

#### Scenario: Resume refused after the answer
- **WHEN** the owner runs `tuckerbox answer add-login --resume "use plan B"` while the change's approval settings are stale
- **THEN** the answer is recorded, the resume is refused with its reason, and the run stays `blocked`

### Requirement: Answering a waiting terminal agent
When a run's step runs in a terminal and its agent is `waiting for you`, `tuckerbox answer <change> <text>` SHALL type the text into the agent's terminal and press enter (`hc send`), and record it as an `answered` history event. The run SHALL stay running.

#### Scenario: Answer without opening the tab
- **WHEN** `add-login`'s review agent waits in its terminal and the owner runs `tuckerbox answer add-login "yes, check the README too"`
- **THEN** the review agent receives that text as the person's message, history has an `answered` event, and the run stays running

### Requirement: Answering from the board
A blocked card, or a card whose terminal agent waits for you, SHALL show the question and a text box in card detail. Answer SHALL run `tuckerbox answer <change> -` with the text on standard input; on a blocked card, Answer and resume SHALL add `--resume`. The board SHALL show the command's result.

#### Scenario: Answer and resume from the card
- **WHEN** the owner types a decision on `fix-board-safety`'s blocked card and clicks Answer and resume
- **THEN** the board runs `tuckerbox answer fix-board-safety --resume -` with that text, and the card moves on from blocked on the next read
