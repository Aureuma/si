# Dyad Codex Loop (Critic → Actor)

This is the core dyad mechanism that proves the Critic can:
- read the Actor’s stdout (via Docker logs),
- decide the next prompt based on that output,
- send the next prompt to Codex via stdin,
- repeat across multiple turns until completion.

## How it works
- Critic polls Actor logs (`/containers/<actor>/logs`) and demuxes output.
- For Codex “turns”, Critic execs into the Actor and runs Codex non-interactively:
  - first turn: `codex exec ...`
  - subsequent turns: `codex exec resume <thread_id> ...`
- Prompts are sent via stdin piping (base64 → `base64 -d` → `codex exec ... -`).
- Each turn is prepended with a short “Dyad Context” preamble (dyad + department + target actor container),
  so Codex has stable role context even across multiple dyads and restarts.
- Codex output is JSONL (`--json`) and is also written into Actor stdout (`tee /proc/1/fd/1`),
  so the Critic can reliably “see” what happened via Docker logs.
- If a dyad task includes `complexity` (or sets `[task.complexity]=...` in notes),
  the Critic will choose model + reasoning effort using the complexity mapping in `docs/codex-model-policy.md`.

## Task kinds
- `test.codex_loop`: built-in 3-turn proof loop (`TURN1_OK → TURN2_OK → TURN3_OK`).
  - The Critic chooses turn 2 based on the output of turn 1, and turn 3 based on turn 2.
  - Task notes store state like:
    - `[codex.thread_id]=...`
    - `[codex_test.phase]=...`
    - `[codex_test.last]=...`
    - `[codex_test.result]=ok`

## Implementation
- Codex turn runner: `agents/critic/internal/codex_loop.go`
- Beam/task dispatcher: `agents/critic/internal/beams.go`
- Log + status reporter (preserves state lines in notes): `agents/critic/internal/monitor.go`
