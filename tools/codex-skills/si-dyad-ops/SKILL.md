---
name: si-dyad-ops
description: Use this skill for operating SI dyads (`si dyad ...`) including spawn/status/peek/logs/exec and offline fake-codex smoke validation.
---

# SI Dyad Ops

Use this workflow for day-to-day dyad operations.

## Spawn and inspect

```bash
si dyad spawn <name> [role]
si dyad status <name>
si dyad logs --member actor <name> --tail 200
si dyad logs --member critic <name> --tail 200
```

## Interactive inspection

```bash
si dyad peek <name>
si dyad exec --member actor <name> -- <cmd...>
si dyad exec --member critic <name> -- <cmd...>
```

## Lifecycle

```bash
si dyad restart <name>
si dyad stop <name>
si dyad start <name>
si dyad remove <name>
```

## Offline smoke mode

For deterministic tests without Codex auth:

```bash
export DYAD_CODEX_START_CMD='cd /workspace && exec /workspace/tools/dyad/fake-codex.sh'
export DYAD_LOOP_ENABLED=1
export DYAD_LOOP_MAX_TURNS=1
si dyad spawn <name> --skip-auth
```

## Guardrails

- Validate mounts when behavior differs from host:
  - `~/.si` should be present in dyad containers for full `si vault` support.
- If loop stalls, inspect tmux readiness and latest reports before recreating.
