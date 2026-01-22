# Dyad Codex Loop (Critic → Actor)

This is the core dyad mechanism that proves the Critic can:
- read the Actor’s stdout (via Docker logs),
- decide the next prompt based on that output,
- send the next prompt to Codex via stdin,
- repeat across multiple turns until completion.

## How it works
- Critic polls Actor logs (`/containers/<actor>/logs`) and demuxes output.
- For `codex.exec` tasks, Critic can append the recent Actor log tail to the next prompt (controlled by `CODEX_ACTOR_LOG_LINES` / `CODEX_ACTOR_LOG_BYTES`; cursor stored as `[actor.logs.since]=...` in task notes).
- For Codex “turns”, Critic execs into the Actor and runs the interactive CLI:
  - first turn: `codex ... "<prompt>"`
  - subsequent turns: `codex resume <session_id> "<prompt>"`
- Prompts are passed as a single argument (with base64 decode inside the exec shell to preserve newlines).
- Each turn is prepended with a short “Dyad Context” preamble (dyad + department + target actor container),
  so Codex has stable role context even across multiple dyads and restarts.
- Codex output is plain text; the Critic captures the tail and uses the last stable line for task progression.
- If a dyad task includes `complexity` (or sets `[task.complexity]=...` in notes),
  the Critic will choose model + reasoning effort using the complexity mapping in `docs/codex-model-policy.md`.

## Task kinds
- `test.codex_loop`: built-in 3-turn proof loop (`TURN1_OK → TURN2_OK → TURN3_OK`).
  - The Critic chooses turn 2 based on the output of turn 1, and turn 3 based on turn 2.
  - Task notes store state like:
    - `[codex.session_id]=...` (interactive session)
    - `[codex_test.phase]=...`
    - `[codex_test.last]=...`
    - `[codex_test.result]=ok`

## Implementation
- Codex turn runner: `agents/critic/internal/codex_loop.go`
- Task dispatcher: `agents/critic/internal/dyad_tasks.go`
- Log + status reporter (preserves state lines in notes): `agents/critic/internal/monitor.go`
