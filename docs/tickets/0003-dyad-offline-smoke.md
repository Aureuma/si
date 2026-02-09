# Ticket 0003: Offline Dyad Smoke Test (No Codex Auth)

## Problem

Developers need a way to validate dyad mechanics (tmux, prompt detection, parsing, turn-taking) without requiring a live Codex login.

## Acceptance Criteria

- Provide a simple interactive "fake codex" executable in the repo that:
  - shows a `›` prompt
  - reads prompts from stdin
  - emits a delimited work report with the dyad markers
- Dyad loop can be configured to start that executable instead of `codex` via a single env var.
- Document the exact commands to run a 1-turn offline dyad and peek into it mid-turn.

