## Resource controls and monitoring

- **Kubernetes limits**: core services should set CPU/memory requests/limits in their Deployments to prevent runaway usage.
- **Guard script**: `bin/resource-guard.sh [--once]` watches `kubectl top` and alerts when CPU or memory exceed thresholds (defaults: 80%).
  - Env: `CPU_THRESHOLD`, `MEM_THRESHOLD`, `TELEGRAM_NOTIFY_URL`, `TELEGRAM_CHAT_ID`.
  - Run via cron or supervisor to get periodic Telegram pings for hotspots.
- **Best practices**
  - Keep per-app DBs lean; drop unused databases (`bin/app-db-shared.sh drop <app>`).
  - Prefer per-app limits when spawning additional dyads; inherit limits from base deployments.
  - Review `health-monitor.sh` + `report-status.sh` output alongside resource guard alerts for holistic state.
  - Increase limits only when justified by workload; document changes in manager feedback.
