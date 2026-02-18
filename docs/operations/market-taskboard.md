# Shared Market Taskboard

The shared market taskboard is the single board for opportunity-to-execution
planning across PaaS initiatives.

## Data Source

- Canonical JSON: `tickets/taskboard/shared-taskboard.json`
- Human-readable board: `tickets/taskboard/SHARED_TASKBOARD.md`
- Auto-generated tickets: `tickets/market-research/`

## Column Model

1. `market-intel`: raw market signals under active review.
2. `paas-backlog`: validated opportunities queued for implementation.
3. `paas-build`: currently being implemented.
4. `validate`: shipped but under KPI/feedback validation.
5. `done`: fully delivered and validated.

## PaaS Integration

ReleaseMind exposes this board for operators in two places:

- API: `GET /api/paas/taskboard`
- Dashboard: `/dashboard/taskboard`

The board is read-only in-app and is maintained by the market-research agent
and engineering workflow updates.
