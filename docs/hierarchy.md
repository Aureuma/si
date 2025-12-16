# Dyad Hierarchy and Reporting

## Departments and dyads
- Engineering: web, backend, infra
- Research: research
- Marketing: marketing
- Security: creds (credentials oversight)

Spawn dyads with `bin/spawn-dyad.sh <name> [role] [department]`; labels/env set:
- Labels: `silexa.dyad=<name>`, `silexa.department=<department>`, `silexa.role=<role>`
- Env: `ROLE`, `DEPARTMENT`

## Control helpers
- `bin/dyadctl.sh create <name> [role] [dept]` — spawn and log feedback
- `bin/dyadctl.sh destroy <name> [reason]` — teardown and log feedback
- `bin/dyadctl.sh list` — list running dyads
- `bin/dyadctl.sh status <name>` — show containers for a dyad
- `bin/report-status.sh` — emoji-bar summary of tasks/access/feedback; optional Telegram send
- `bin/escalate-blockers.sh` — escalate open tasks/pending access via feedback/Telegram
- `bin/review-cron.sh` — high-stakes review snapshot (emoji-prefixed) for scheduled runs
- Profiles: actor/critic starting contexts live in `profiles/`; print with `bin/actor-context.sh <profile>` (e.g., actor-web, actor-infra, critic-web, critic-infra, critic-qa, critic-research).
- Capability office: model/tool recommendations per role via `bin/dyad-capability.sh <role>` (see `docs/capability-office.md` for scope and guardrails).
- MCP exploration unit: scout MCP servers with `bin/mcp-scout.sh`, record recommendations in manager `/feedback`, and coordinate adoption via gateway catalog updates (see `docs/mcp-exploration.md`).
- Management comms: use `bin/management-broadcast.sh "<message>" [severity]` for cross-department updates (posts to manager feedback and Telegram). See `docs/management-comms.md`.

## Reporting paths
- Heartbeats: Critics -> Manager `/beats`.
- Feedback: POST `/feedback` (`bin/add-feedback.sh {severity,message,source,context}`) persisted and reviewable.
- Human tasks: POST `/human-tasks` (`bin/add-human-task.sh ...`), persisted, optional Telegram notify.
- Access requests: POST `/access-requests` (`bin/request-access.sh ...`), resolve via `/access-requests/resolve` (`bin/resolve-access.sh`).
- Telegram: bot at `:8081/notify` + `/human-task`; Manager uses `TELEGRAM_NOTIFY_URL`/`TELEGRAM_CHAT_ID` for alerts.

## Oversight expectations
- Security/creds dyad reviews access requests and sensitive feedback; can resolve/deny via `bin/resolve-access.sh`.
- Department leads (human) review `/feedback` and `/human-tasks` and act on items relevant to their dyads.

## Usage quick refs
- List dyads: `sudo bin/list-dyads.sh` or `sudo bin/dyadctl.sh list`.
- File access request: `bin/request-access.sh "actor-infra" "secrets/telegram_bot_token" "read" "reason" "security"`.
- Resolve access: `bin/resolve-access.sh <id> approved|denied [by] [notes]`.
- File feedback: `bin/add-feedback.sh warn "issue" source "context"`.
- File human task: `bin/add-human-task.sh "title" "commands" "url" "timeout" "requested_by" "notes"` or Telegram `/human-task`.
