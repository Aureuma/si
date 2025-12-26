# Maintenance & Vital Signs

## Health checks
- Manager: `curl -fsSL http://localhost:9090/healthz`
- Telegram bot: `curl -fsSL http://localhost:8081/healthz`
- Resource brokers: `http://localhost:9091/healthz`, `http://localhost:9092/healthz`
- Critic heartbeats: `curl -fsSL http://localhost:9090/beats` (ensure recent timestamps)
- Status snapshot: `bin/report-status.sh` (optional Telegram)
- Escalation: `bin/escalate-blockers.sh`
- Pre-deploy cost/risk gate: `bin/pre-deploy-check.sh` (summarizes tasks/access/resource/infra requests; optional Telegram)

## Garbage collection
- `bin/cleanup-dyads.sh`: removes stopped dyad containers and prunes dangling images.
  - Safe to run periodically via cron/systemd.

## Persistence
- Manager tasks/feedback/access/metrics persisted in Temporal.
- Brokers persisted in `data/resource-broker/requests.json` and `data/infra-broker/infra_requests.json`.

## Access and secrets
- Use access requests for sensitive resources; resolve via security dyad/human.
- Telegram bot token rotation: `bin/rotate-telegram-token.sh <new_token>`.

## Dyad controls
- Register/spawn/destroy/list/status: `bin/dyadctl.sh` (register before spawn; labels applied for filtering/cleanup).
- Web team: `bin/spawn-web-team.sh` to provision planner/builder/QA dyads.

## Alerts and schedules (suggested)
- Daily: `report-status.sh`, `health-monitor.sh` (sane thresholds), `cleanup-dyads.sh`.
- Weekly: `review-cron.sh`, `escalate-blockers.sh`.
- Pre-deploy: `pre-deploy-check.sh` with Telegram to gate costly/infra actions.
