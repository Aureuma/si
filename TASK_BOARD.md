# Task Board

This file is the shared work queue for dyads. It is mounted inside dyad containers at `/workspace/TASK_BOARD.md`.

Rules (human + dyad):
- Add new tasks to **Backlog**.
- A dyad actor should pick exactly one task at a time, move it to **Doing**, and update it each turn.
- When finished, move the task to **Done**.
- The dyad loop also appends a short entry to **Turn Log** after each actor turn (best-effort).

## Backlog

- [ ] T-003 DYAD-001 Fix host settings bind mount inside dyad/codex containers. Status: TODO
- [ ] T-004 DYAD-002 Add offline dyad e2e smoke docs. Status: TODO

## Doing

## Done

- [x] T-002 Add/adjust a small repo test/doc improvement identified by the critic. Status: DONE (2026-02-11)
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
- 2026-02-10T15:07:57Z 🪢 testdyad 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T15:07:57Z 🪢 testdyad 🛩️ actor Turn 2: ACTOR REPORT TURN 2
- 2026-02-10T15:07:57Z 🪢 testdyad 🛩️ actor Turn 3: ACTOR REPORT TURN 3
- 2026-02-10T15:07:57Z 🪢 seedstop 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T19:03:01Z 🪢 testdyad 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-10T19:03:01Z 🪢 testdyad 🛩️ actor Turn 2: ACTOR REPORT TURN 2
- 2026-02-10T19:03:01Z 🪢 testdyad 🛩️ actor Turn 3: ACTOR REPORT TURN 3
- 2026-02-10T19:03:01Z 🪢 seedstop 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-11T15:05:54Z 🪢 testdyad 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-11T15:05:54Z 🪢 testdyad 🛩️ actor Turn 2: ACTOR REPORT TURN 2
- 2026-02-11T15:05:54Z 🪢 testdyad 🛩️ actor Turn 3: ACTOR REPORT TURN 3
- 2026-02-11T15:05:54Z 🪢 seedstop 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-11T15:06:19Z 🪢 testdyad 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-11T15:06:19Z 🪢 testdyad 🛩️ actor Turn 2: ACTOR REPORT TURN 2
- 2026-02-11T15:06:19Z 🪢 testdyad 🛩️ actor Turn 3: ACTOR REPORT TURN 3
- 2026-02-11T15:06:19Z 🪢 seedstop 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-11T19:40:11Z 🪢 wand 🛩️ actor Turn 1: • I only got b. What do you want me to do in /workspace?
- 2026-02-11T19:55:53Z 🪢 go 🛩️ actor Turn 1: • Exploring
- 2026-02-11T19:55:58Z 🪢 go 🛩️ actor Turn 2: 
- 2026-02-11T19:56:10Z 🪢 go 🛩️ actor Turn 3: Picked T-002, updated docs/testing.md static analysis notes (`--no-fail`, CLI module scope), and marked task done.
- 2026-02-11T19:56:04Z 🪢 go 🛩️ actor Turn 3: • Explored
- 2026-02-11T19:56:09Z 🪢 go 🛩️ actor Turn 4: • Explored
- 2026-02-11T22:01:21Z 🪢 testdyad 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-11T22:01:21Z 🪢 testdyad 🛩️ actor Turn 2: ACTOR REPORT TURN 2
- 2026-02-11T22:01:21Z 🪢 testdyad 🛩️ actor Turn 3: ACTOR REPORT TURN 3
- 2026-02-11T22:01:21Z 🪢 seedstop 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-11T22:05:28Z 🪢 testdyad 🛩️ actor Turn 1: ACTOR REPORT TURN 1
- 2026-02-11T22:05:28Z 🪢 testdyad 🛩️ actor Turn 2: ACTOR REPORT TURN 2
- 2026-02-11T22:05:28Z 🪢 testdyad 🛩️ actor Turn 3: ACTOR REPORT TURN 3
- 2026-02-11T22:05:28Z 🪢 seedstop 🛩️ actor Turn 1: ACTOR REPORT TURN 1
