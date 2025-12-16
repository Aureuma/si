# Web App Delivery Flow (Dyads)

## Team composition
- Planner dyad: `web-planner` (department: planning) — scopes features, breaks down tasks.
- Builder dyad: `web-builder` (department: engineering) — implements web app.
- QA dyad: `web-qa` (department: qa) — smoke and regression checks.
- Security dyad: `creds` — reviews access requests and sensitive changes.

## Workflow
1) Planner creates tasks in Manager (`bin/add-human-task.sh` or Telegram `/human-task`) for specs, architecture, and acceptance criteria.
2) Builder picks tasks, creates access requests if needed (e.g., secrets, domains), and files status/feedback.
3) QA runs smoke tests (see `docs/testing.md`) and posts feedback.
4) Status broadcasts via `bin/report-status.sh` (Telegram) and `bin/escalate-blockers.sh`.
5) Access approvals handled by security via `/access-requests`.

## Quick commands
- Spawn team: `bin/spawn-web-team.sh` (planner/builder/qa dyads).
- List dyads: `sudo bin/dyadctl.sh list`.
- Create task (Telegram bot): POST to `http://localhost:8081/human-task` with `title/commands/...`.
- Status report: `TELEGRAM_CHAT_ID=<id> ./bin/report-status.sh`.
- Escalate: `TELEGRAM_CHAT_ID=<id> ./bin/escalate-blockers.sh`.

## Notes
- Use `apps/` for app repos; sample service: `apps/sample-go-service`.
- Critics monitor logs/heartbeats; adjust `CRITIC_LOG_INTERVAL`/`CRITIC_BEAT_INTERVAL` per dyad if needed.
- Keep secrets via access requests; do not mount secrets into builders unless approved.
