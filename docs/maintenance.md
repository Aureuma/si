# Maintenance & Vital Signs

## Health checks
- Manager: `curl -fsSL http://localhost:9090/healthz`
- Telegram bot: `curl -fsSL http://localhost:8081/healthz`
- Resource brokers: `http://localhost:9091/healthz`, `http://localhost:9092/healthz`
- Critic heartbeats: `curl -fsSL http://localhost:9090/beats` (ensure recent timestamps)
- Status snapshot: `silexa report status` (optional Telegram)
- Escalation: `silexa report escalate`
- Pre-deploy cost/risk gate: review open tasks/access/feedback and run a manual Pulumi/Terraform preview.

## Garbage collection
- `silexa dyad cleanup`: removes stopped dyad containers and prunes dangling images.
  - Safe to run periodically via cron/systemd.

## Persistence
- Manager tasks/feedback/access/metrics persisted on disk (volume mounted at `/data`).
- Brokers persisted in Docker volumes (`silexa-resource-broker-data`, `silexa-infra-broker-data`).

## Access and secrets
- Use access requests for sensitive resources; resolve via security dyad/human.
- Telegram bot token rotation: update `secrets/telegram_bot_token` and restart the bot (`docker restart silexa-telegram-bot`).

## Dyad controls
- Register/spawn/destroy/list/status: `silexa dyad` (register before spawn; labels applied for filtering/cleanup).
- Web team: `silexa dyad spawn` to provision planner/builder/QA dyads.

## Alerts and schedules (suggested)
- Daily: `silexa report status`, `silexa dyad cleanup`.
- Weekly: `silexa report review`, `silexa report escalate`.
- Pre-deploy: review metrics, access requests, and run infra previews.
