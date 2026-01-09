# Dyad Hierarchy and Reporting

## Departments and dyads
- Engineering: web, backend, infra
- Research: research
- Marketing: marketing
- Security: silexa-credentials (credentials oversight)

Spawn dyads with `silexa dyad spawn [--temporal] <name> [role] [department]`; labels/env set:
- Labels: `silexa.dyad=<name>`, `silexa.department=<department>`, `silexa.role=<role>`
- Env: `ROLE`, `DEPARTMENT`

## Control helpers
- Dyad roster: `silexa roster apply` (sync roster to manager), `silexa roster status` (status table). See `docs/dyad-roster.md`.
- `silexa dyad spawn <name> [role] [dept]` — spawn a dyad
- `silexa dyad remove <name>` — teardown a dyad
- `silexa dyad list` — list running dyads
- `silexa dyad status <name>` — show containers for a dyad
- `silexa report status` — emoji-bar summary of tasks/access/feedback; optional Telegram send
- `silexa report escalate` — escalate open tasks/pending access via feedback/Telegram
- `silexa report review` — high-stakes review snapshot (emoji-prefixed) for scheduled runs
- Profiles: actor/critic starting contexts live in `profiles/`; print with `silexa profile <profile>` (e.g., actor-web, actor-infra, critic-web, critic-infra, critic-qa, critic-research).
- Capability office: model/tool recommendations per role via `silexa capability <role>` (see `docs/capability-office.md` for scope and guardrails).
- MCP exploration unit: scout MCP servers with `silexa mcp scout`, record recommendations in manager `/feedback`, and coordinate adoption via gateway catalog updates (see `docs/mcp-exploration.md`).
- Management comms: use `silexa feedback broadcast "<message>" [severity]` for cross-department updates (posts to manager feedback and Telegram). See `docs/management-comms.md`.

## Reporting paths
- Heartbeats: Critics -> Manager `/beats`.
- Feedback: POST `/feedback` (`silexa feedback add {severity,message,source,context}`) persisted and reviewable.
- Human tasks: POST `/human-tasks` (`silexa human add ...`), persisted, optional Telegram notify.
- Access requests: POST `/access-requests` (`silexa access request ...`), resolve via `/access-requests/resolve` (`silexa access resolve`).
- Telegram: bot at `:8081/notify` + `/human-task`; Manager uses `TELEGRAM_NOTIFY_URL`/`TELEGRAM_CHAT_ID` for alerts.

## Oversight expectations
- `silexa-credentials` reviews access requests and sensitive feedback; resolves via `credentials.resolve_request` or `silexa access resolve`.
- Department leads (human) review `/feedback` and `/human-tasks` and act on items relevant to their dyads.

## Usage quick refs
- List dyads: `silexa dyad list`.
- File access request: `silexa access request "actor-infra" "secrets/telegram_bot_token" "read" "reason" "security"`.
- Resolve access: `silexa access resolve <id> approved|denied [by] [notes]`.
- File feedback: `silexa feedback add warn "issue" source "context"`.
- File human task: `silexa human add "title" "commands" "url" "timeout" "requested_by" "notes"` or Telegram `/human-task`.
