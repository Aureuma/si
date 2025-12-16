# Dyad Hierarchy and Reporting

## Departments and dyads
- Engineering: web, backend, infra
- Research: research
- Marketing: marketing
- Security: creds (credentials oversight)

Spawn dyads with `bin/spawn-dyad.sh <name> [role] [department]`; labels/env set:
- Labels: `silexa.dyad=<name>`, `silexa.department=<department>`, `silexa.role=<role>`
- Env: `ROLE`, `DEPARTMENT`

## Reporting paths
- Heartbeats: Critics -> Manager `/beats`.
- Feedback: POST `/feedback` (`bin/add-feedback.sh {severity,message,source,context}`) persisted and reviewable.
- Human tasks: POST `/human-tasks` (`bin/add-human-task.sh ...`), persisted, optional Telegram notify.
- Access requests: POST `/access-requests` (`bin/request-access.sh ...`), resolve via `/access-requests/resolve` (`bin/resolve-access.sh`).
- Telegram: bot at `:8081/notify`; Manager uses `TELEGRAM_NOTIFY_URL`/`TELEGRAM_CHAT_ID` for alerts.

## Oversight expectations
- Security/creds dyad reviews access requests and sensitive feedback; can resolve/deny via `bin/resolve-access.sh`.
- Department leads (human) should review `/feedback` and `/human-tasks` and act on items relevant to their dyads.

## Usage quick refs
- List dyads: `sudo bin/list-dyads.sh`.
- File access request: `bin/request-access.sh "actor-infra" "secrets/telegram_bot_token" "read" "reason" "security"`.
- Resolve access: `bin/resolve-access.sh <id> approved|denied [by] [notes]`.
- File feedback: `bin/add-feedback.sh warn "issue" source "context"`.
- File human task: `bin/add-human-task.sh "title" "commands" "url" "timeout" "requested_by" "notes"`.
