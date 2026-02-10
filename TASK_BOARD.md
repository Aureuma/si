# Task Board

This file is the shared work queue for dyads. It is mounted inside dyad containers at `/workspace/TASK_BOARD.md`.

Rules (human + dyad):
- Add new tasks to **Backlog**.
- A dyad actor should pick exactly one task at a time, move it to **Doing**, and update it each turn.
- When finished, move the task to **Done**.
- The dyad loop also appends a short entry to **Turn Log** after each actor turn (best-effort).

## Backlog

- [ ] T-002 Add/adjust a small repo test/doc improvement identified by the critic. Status: TODO
- [ ] T-003 DYAD-001 Fix host settings bind mount inside dyad/codex containers. Status: TODO
- [ ] T-004 DYAD-002 Add offline dyad e2e smoke docs. Status: TODO

## Doing

## Done

- [x] T-001 Verify dyad can read `si vault` safely (run `si vault status` and `si vault list`; do not print secret values). Status: DONE (2026-02-09)

## Turn Log

<!--
Format:
- 2026-02-09T00:00:00Z 🪢 <dyad> 🛩️ actor Turn N: <what changed>
-->
- 2026-02-09T23:49:45Z 🪢 testdyad 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-09T23:49:45Z 🪢 testdyad 🛩️ actor Turn 2: ACTOR REPORT TURN 2
- 2026-02-09T23:49:45Z 🪢 testdyad 🛩️ actor Turn 3: ACTOR REPORT TURN 3
- 2026-02-09T23:49:45Z 🪢 seedstop 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T03:02:40Z 🪢 testdyad 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T03:02:40Z 🪢 testdyad 🛩️ actor Turn 2: ACTOR REPORT TURN 2
- 2026-02-10T03:02:40Z 🪢 testdyad 🛩️ actor Turn 3: ACTOR REPORT TURN 3
- 2026-02-10T03:02:40Z 🪢 seedstop 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T03:15:29Z 🪢 testdyad 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T03:15:29Z 🪢 testdyad 🛩️ actor Turn 2: ACTOR REPORT TURN 2
- 2026-02-10T03:15:29Z 🪢 testdyad 🛩️ actor Turn 3: ACTOR REPORT TURN 3
- 2026-02-10T03:15:29Z 🪢 seedstop 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T03:24:45Z 🪢 testdyad 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T03:24:45Z 🪢 testdyad 🛩️ actor Turn 2: ACTOR REPORT TURN 2
- 2026-02-10T03:24:45Z 🪢 testdyad 🛩️ actor Turn 3: ACTOR REPORT TURN 3
- 2026-02-10T03:24:45Z 🪢 seedstop 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T03:25:00Z 🪢 testdyad 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T03:25:00Z 🪢 testdyad 🛩️ actor Turn 2: ACTOR REPORT TURN 2
- 2026-02-10T03:25:00Z 🪢 testdyad 🛩️ actor Turn 3: ACTOR REPORT TURN 3
- 2026-02-10T03:25:00Z 🪢 seedstop 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T03:30:36Z 🪢 testdyad 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T03:30:36Z 🪢 testdyad 🛩️ actor Turn 2: ACTOR REPORT TURN 2
- 2026-02-10T03:30:36Z 🪢 testdyad 🛩️ actor Turn 3: ACTOR REPORT TURN 3
- 2026-02-10T03:30:36Z 🪢 seedstop 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T03:40:18Z 🪢 testdyad 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T03:40:18Z 🪢 testdyad 🛩️ actor Turn 2: ACTOR REPORT TURN 2
- 2026-02-10T03:40:18Z 🪢 testdyad 🛩️ actor Turn 3: ACTOR REPORT TURN 3
- 2026-02-10T03:40:18Z 🪢 seedstop 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T03:55:25Z 🪢 testdyad 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T03:55:25Z 🪢 testdyad 🛩️ actor Turn 2: ACTOR REPORT TURN 2
- 2026-02-10T03:55:25Z 🪢 testdyad 🛩️ actor Turn 3: ACTOR REPORT TURN 3
- 2026-02-10T03:55:25Z 🪢 seedstop 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T03:59:13Z 🪢 testdyad 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T03:59:13Z 🪢 testdyad 🛩️ actor Turn 2: ACTOR REPORT TURN 2
- 2026-02-10T03:59:13Z 🪢 testdyad 🛩️ actor Turn 3: ACTOR REPORT TURN 3
- 2026-02-10T03:59:13Z 🪢 seedstop 🛩️ actor Turn 1: ACTOR REPORT TURN 1
