## Inter-management communication

Goal: keep department leads and management aligned with minimal friction.

### Channels
- **Telegram bot**: primary human channel. Use for urgent status, blockers, deploy notices, and approvals. `/status`, `/tasks`, replies captured as feedback.
- **Manager API**: `/feedback` for structured notes; `/human-tasks` for actionable asks; `/metrics` for performance signals. Store the source (`dept/lead`) for traceability.
- **Periodic reports**: `bin/report-status.sh` and `bin/review-cron.sh` for scheduled summaries; send to Telegram.

### Broadcast helper
- Use `bin/management-broadcast.sh "<message>" [severity]` to fan out: it posts to manager feedback and optionally Telegram (if `TELEGRAM_NOTIFY_URL` is set).
- Severity options: info|warn|error (default info).

### Expectations
- Keep updates concise: headline, impact, ask/decision needed, next check-in.
- Use Telegram only for human-readable summaries; keep structured data in manager (feedback/tasks/metrics).
- For cross-department coordination, include department tags (e.g., `[web]`, `[infra]`, `[security]`).
