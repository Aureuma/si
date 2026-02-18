# PaaS Market Taskboard

The shared taskboard tracks market opportunities from signal discovery through delivery.

## Canonical Files

- `tickets/taskboard/shared-taskboard.json`
- `tickets/taskboard/SHARED_TASKBOARD.md`
- `tickets/market-research/*.md`

## Column Model

1. `market-intel`: new market signals under review.
2. `paas-backlog`: validated opportunities queued for implementation.
3. `paas-build`: currently in implementation.
4. `validate`: shipped work under KPI validation.
5. `done`: validated and closed opportunities.

## CLI Integration

Use `si paas taskboard`:

```bash
si paas taskboard show
si paas taskboard list --status paas-backlog --priority P1
si paas taskboard add --title "Example task" --status paas-backlog --priority P2
si paas taskboard move --id <task-id> --status validate
```

All commands support `--json`.

## Path Resolution

Taskboard path resolution order:

1. `SI_PAAS_TASKBOARD_PATH` (explicit override).
2. Repository path under `tickets/taskboard/`.
3. Fallback state path under the current PaaS context.
