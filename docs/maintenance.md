# Maintenance & Vital Signs

## Health checks
- Manager: `curl -fsSL http://localhost:9090/healthz` (add when available)
- Telegram bot: `curl -fsSL http://localhost:8081/healthz`
- Critic heartbeats: `curl -fsSL http://localhost:9090/beats` (ensure recent timestamps)
- Status snapshot: `bin/report-status.sh` (optional Telegram)
- Escalation: `bin/escalate-blockers.sh`

## Garbage collection
- `bin/cleanup-dyads.sh`: removes stopped dyad containers and prunes dangling images.
  - Safe to run periodically via cron/systemd.

## Persistence
- Manager tasks/feedback/access are persisted in `data/manager/tasks.json`. Ensure volume is mounted.

## Access and secrets
- Use access requests for sensitive resources; resolve via security dyad/human.
- Telegram bot token rotation: `bin/rotate-telegram-token.sh <new_token>`.

## Dyad controls
- Spawn/destroy/list/status: `bin/dyadctl.sh` (labels applied for filtering/cleanup).
- Web team: `bin/spawn-web-team.sh` to provision planner/builder/QA dyads.

## Alerts
- Telegram chat configured via `.env` (`TELEGRAM_CHAT_ID`); `NOTIFY_URL` points to bot `/notify`.
- Consider scheduling `bin/report-status.sh` and `bin/escalate-blockers.sh` for periodic reporting.
