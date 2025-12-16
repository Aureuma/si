# Dyad Hierarchy and Reporting

## Departments and dyads
- Engineering: web, backend, infra
- Research: research
- Marketing: marketing

Each dyad is spawned with `bin/spawn-dyad.sh <name> [role] [department]` and carries labels/env:
- `silexa.dyad=<name>`
- `silexa.department=<department>`
- `silexa.role=<role>`
- env: `ROLE`, `DEPARTMENT`

## Reporting paths
- Critics send heartbeats to Manager (`/beats`).
- Feedback: POST `/feedback` with `{severity, message, source, context}` (use `bin/add-feedback.sh`). Persisted to `data/manager/tasks.json`.
- Human tasks: POST `/human-tasks` (or `bin/add-human-task.sh`); persisted and optionally notified via Telegram.
- Telegram: Bot at `:8081/notify`; Manager notifies via `TELEGRAM_NOTIFY_URL`/`TELEGRAM_CHAT_ID`.

## Usage
- List dyads: `sudo bin/list-dyads.sh`.
- Spawn: `bin/spawn-dyad.sh backend backend engineering`.
- Feedback example: `bin/add-feedback.sh warn "infra actor needs codex login" critic-infra "ssh -N -L ..."`.
- Human task example: `bin/add-human-task.sh "Codex login" "ssh -N -L ..." "http://127.0.0.1:PORT/..." "15m" "silexa-actor-infra" "keep tunnel alive"`.
