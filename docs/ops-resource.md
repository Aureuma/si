## Resource controls and monitoring

- **Swarm limits**: core services have CPU/memory limits set in `docker-stack.yml` to prevent runaway usage:
  - telegram-bot: 0.25 CPU / 128 MiB
  - manager, brokers: 0.5 CPU / 256 MiB
  - critics: 0.75 CPU / 512 MiB
  - actors: 1 CPU / 1 GiB
  - coder-agent: 1.5 CPU / 2 GiB
- **Guard script**: `bin/resource-guard.sh [--once]` watches `docker stats` and alerts when CPU or memory exceed thresholds (defaults: 80%).
  - Env: `CPU_THRESHOLD`, `MEM_THRESHOLD`, `TELEGRAM_NOTIFY_URL`, `TELEGRAM_CHAT_ID`.
  - Run via cron or supervisor to get periodic Telegram pings for hotspots.
- **Best practices**
  - Keep per-app DBs lean; stop unused services (`docker service rm <stack>_db-<app>` or `bin/app-db.sh drop <app>`).
  - Prefer per-app limits when spawning additional dyads; inherit limits from base swarm services.
  - Review `health-monitor.sh` + `report-status.sh` output alongside resource guard alerts for holistic state.
  - Increase limits only when justified by workload; document changes in manager feedback.
