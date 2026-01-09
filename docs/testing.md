# Testing Harness for Dyads

This doc outlines how a dyad (Actor+Critic) can exercise services and infrastructure.

## Test layout
- Go unit tests live inside each module.
- UI/visual checks use Playwright via `tools/visual-runner`.

## Sample Go service
- Location: `apps/sample-go-service`
- Build image: `silexa app build sample-go-service`
- Deploy: `silexa app deploy sample-go-service`
- Health check: `curl -fsSL http://localhost:18080/healthz` (expect `ok`)
- Main endpoint: `curl -fsSL http://localhost:18080/` (expect greeting)
- Cleanup: `silexa app remove sample-go-service`

## Quick run
- Run `go test` per module and use Playwright for visual checks.

## Frameworks and tools
- Go services: `go test ./...` per module.
- Integration tests: curl + JSON asserts (no extra framework).
- UI/visual checks: Playwright in `tools/visual-runner`.

This keeps tooling minimal while still using standard ecosystem tools; add heavier frameworks only when test volume demands it.

## Dyad usage pattern
- Actor steps: build images, deploy with Docker Compose, run curl-driven smoke tests.
- Critic steps: tail actor logs and heartbeat to manager; optional alert via Telegram when tests fail or hang.
- Human loop: if a test requires external input (e.g., OAuth), actor/critic files a human task via manager `/human-tasks` or `silexa human add`.

## Recommended env knobs
- Critics: `CRITIC_LOG_INTERVAL`, `CRITIC_BEAT_INTERVAL` to tune chatter.
- Manager: `TEMPORAL_ADDRESS`, `TEMPORAL_NAMESPACE`, `TEMPORAL_TASK_QUEUE` to connect to Temporal.
- Telegram: `TELEGRAM_NOTIFY_URL=http://telegram-bot:8081/notify`, `TELEGRAM_CHAT_ID=<id>` for human alerts.
