# Testing Harness for Dyads

This doc outlines how a dyad (Actor+Critic) can exercise services and infrastructure.

## Test layout
- `tests/smoke/`: stack health, MCP gateway, QA smoke
- `tests/integration/`: dyad-to-dyad workflow checks (`dyad-communications.sh`)
- `tests/go/`: Go module unit tests
- `tests/visual/`: Playwright-based visual checks
- `tests/run.sh`: scope runner (`smoke`, `go`, `integration`, `visual`)

Legacy entrypoints in `bin/` remain as thin wrappers for compatibility.

## Sample Go service
- Location: `apps/sample-go-service`
- Build image: `cd apps/sample-go-service && docker build -t silexa/sample-go-service:local .`
- Run test container: `docker run -d --name test-sample --network silexa_net -p 18080:8080 silexa/sample-go-service:local`
- Health check: `curl -fsSL http://localhost:18080/healthz` (expect `ok`)
- Main endpoint: `curl -fsSL http://localhost:18080/` (expect greeting)
- Cleanup: `docker stop test-sample && docker rm test-sample`

## Quick run
- Default suite: `tests/run.sh` (or wrapper `bin/tests.sh`)
- Smoke only: `tests/run.sh --scope smoke`
- Visual: `tests/run.sh --scope visual --visual-app <app>`

## QA smoke helper
- `tests/smoke/qa-smoke.sh` (or wrapper `bin/qa-smoke.sh`) uses sample-go-service by default, spins up a container on `silexa_net`, hits `/healthz` and `/`, and reports ✅/❌. Optional Telegram notify via `TELEGRAM_CHAT_ID`/`NOTIFY_URL`.
- Override app image via `APP_IMAGE`; port via `PORT`.

## Frameworks and tools
- Go services: `go test ./...` per module (`tests/go/run-go-tests.sh`).
- Integration tests: bash + curl + python for JSON asserts (no extra framework).
- UI/visual checks: Playwright in `tools/visual-runner`.

This keeps tooling minimal while still using standard ecosystem tools; add heavier frameworks only when test volume demands it.

## Dyad usage pattern
- Actor steps: build images, run containers in `silexa_net` network, run curl-driven smoke tests.
- Critic steps: tail actor logs and heartbeat to manager; optional alert via Telegram when tests fail or hang.
- Human loop: if a test requires external input (e.g., OAuth), actor/critic files a human task via manager `/human-tasks` or `bin/add-human-task.sh`.

## Recommended env knobs
- Critics: `CRITIC_LOG_INTERVAL`, `CRITIC_BEAT_INTERVAL` to tune chatter.
- Manager: `TEMPORAL_ADDRESS`, `TEMPORAL_NAMESPACE`, `TEMPORAL_TASK_QUEUE` to connect to Temporal.
- Telegram: `TELEGRAM_NOTIFY_URL=http://telegram-bot:8081/notify`, `TELEGRAM_CHAT_ID=<id>` for human alerts.
