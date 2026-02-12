# 🪢 Dyad Protocol

This file defines the dyad loop contract between the critic and the actor.

Inside dyad containers, the repo is mounted at `/workspace`.

## Roles

- `🧠 critic`: reviews the actor's work report and sends the next instructions to the actor.
- `🛩️ actor`: performs one small, safe work iteration per turn and reports back.

## Task Source

- Primary queue: `/workspace/TASK_BOARD.md`
- The actor should pick exactly one task at a time and keep it moving.

## Actor Output (Required)

The actor must respond with **only** this delimited work report format:

```
<<WORK_REPORT_BEGIN>>
Summary:
- ...
Changes:
- ...
Validation:
- ...
Open Questions:
- ...
Next Step for Critic:
- ...
⏰ Finished At (UTC): <ISO8601 timestamp>
<<WORK_REPORT_END>>
```

Notes:
- Keep bullets single-level (no nested bullets).
- If you run commands, include the command names and the key results in `Validation:`.
- If you touched tasks, update `/workspace/TASK_BOARD.md` and mention it in `Changes:`.
- Runtime transport note: the critic loop may also request a per-turn tagged variant
  (`<<WORK_REPORT_BEGIN:<turn_id>> ... <<WORK_REPORT_END:<turn_id>>`) to improve
  tmux turn correlation; it is normalized to the standard markers above.

## Secrets / Vault

- Use `si vault` to read secrets if needed.
- Never print secret values into logs or reports.

## Loop Controls (Recovery)

The critic loop watches control files under `/workspace/.si/dyad/<dyad>/`:

- `control.pause`: pause the loop (polls periodically).
- `control.stop`: stop the loop (persists across restarts).
