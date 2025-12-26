# Testing Harness for Dyads

This doc outlines how a dyad (Actor+Critic) can exercise services and infrastructure.

## Sample Go service
- Location: `apps/sample-go-service`
- Build image: `cd apps/sample-go-service && docker build -t silexa/sample-go-service:local .`
- Run test container: `docker run -d --name test-sample --network silexa_net -p 18080:8080 silexa/sample-go-service:local`
- Health check: `curl -fsSL http://localhost:18080/healthz` (expect `ok`)
- Main endpoint: `curl -fsSL http://localhost:18080/` (expect greeting)
- Cleanup: `docker stop test-sample && docker rm test-sample`

## QA smoke helper
- `bin/qa-smoke.sh` (uses sample-go-service by default) spins up a container on `silexa_net`, hits `/healthz` and `/`, and reports ✅/❌. Optional Telegram notify via `TELEGRAM_CHAT_ID`/`NOTIFY_URL`.
- Override app image via `APP_IMAGE`; port via `PORT`.

## Dyad usage pattern
- Actor steps: build images, run containers in `silexa_net` network, run curl-driven smoke tests.
- Critic steps: tail actor logs and heartbeat to manager; optional alert via Telegram when tests fail or hang.
- Human loop: if a test requires external input (e.g., OAuth), actor/critic files a human task via manager `/human-tasks` or `bin/add-human-task.sh`.

## Recommended env knobs
- Critics: `CRITIC_LOG_INTERVAL`, `CRITIC_BEAT_INTERVAL` to tune chatter.
- Manager: `DATA_DIR=/data` mounted to persist tasks.
- Telegram: `TELEGRAM_NOTIFY_URL=http://telegram-bot:8081/notify`, `TELEGRAM_CHAT_ID=<id>` for human alerts.
