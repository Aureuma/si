# Ticket 0002: Strict Report Parsing + Long Output Robustness

## Problem

Dyad turns must be parseable and reliable across:

- short outputs
- very long outputs (many thousands of lines)

## Acceptance Criteria

- Default behavior requires work reports to be delimited by:
  - `<<WORK_REPORT_BEGIN>>`
  - `<<WORK_REPORT_END>>`
- Add a tunable `DYAD_LOOP_TMUX_CAPTURE_LINES` so long-running sessions do not require capturing the entire tmux history.
- When strict mode is enabled and Codex returns to a ready prompt without emitting a delimited report, the turn fails fast (so retries happen quickly).

## Test Coverage

- A tmux-backed unit/integration test that exercises:
  - short output with markers
  - long output where the report is at the end and only the tail is captured
  - strict mode rejecting undelimited output

