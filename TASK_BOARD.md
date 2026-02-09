# Task Board

This file is the shared work queue for dyads. It is mounted inside dyad containers at `/workspace/TASK_BOARD.md`.

Rules (human + dyad):
- Add new tasks to **Backlog**.
- A dyad actor should pick exactly one task at a time, move it to **Doing**, and update it each turn.
- When finished, move the task to **Done**.
- The dyad loop also appends a short entry to **Turn Log** after each actor turn (best-effort).

## Backlog

- [ ] T-001 Verify dyad can read `si vault` safely (run `si vault status` and `si vault list`; do not print secret values). Status: TODO
- [ ] T-002 Add/adjust a small repo test/doc improvement identified by the critic. Status: TODO

## Doing

## Done

## Turn Log

<!--
Format:
- 2026-02-09T00:00:00Z 🪢 <dyad> 🛩️ actor Turn N: <what changed>
-->
