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
- Build image: `bin/app-build.sh sample-go-service` (push/load into your cluster as needed)
- Deploy: `bin/app-deploy.sh sample-go-service`
- Port-forward: `kubectl -n silexa port-forward svc/sample-go-service 18080:8080`
- Health check: `curl -fsSL http://localhost:18080/healthz` (expect `ok`)
- Main endpoint: `curl -fsSL http://localhost:18080/` (expect greeting)
- Cleanup: `bin/app-remove.sh sample-go-service`

## Quick run
- Default suite: `tests/run.sh` (or wrapper `bin/tests.sh`)
- Smoke only: `tests/run.sh --scope smoke`
- Visual: `tests/run.sh --scope visual --visual-app <app>`

## QA smoke helper
- `tests/smoke/qa-smoke.sh` (or wrapper `bin/qa-smoke.sh`) uses sample-go-service by default, port-forwards the service, hits `/healthz` and `/`, and reports ✅/❌. Optional Telegram notify via `TELEGRAM_CHAT_ID`/`NOTIFY_URL`.
- Override port via `PORT` or service via `SERVICE_NAME`.

## Frameworks and tools
- Go services: `go test ./...` per module (`tests/go/run-go-tests.sh`).
- Integration tests: bash + curl + python for JSON asserts (no extra framework).
- UI/visual checks: Playwright in `tools/visual-runner`.

This keeps tooling minimal while still using standard ecosystem tools; add heavier frameworks only when test volume demands it.

## Dyad usage pattern
- Actor steps: build images, deploy to Kubernetes, run curl-driven smoke tests.
- Critic steps: tail actor logs and heartbeat to manager; optional alert via Telegram when tests fail or hang.
- Human loop: if a test requires external input (e.g., OAuth), actor/critic files a human task via manager `/human-tasks` or `bin/add-human-task.sh`.

## Recommended env knobs
- Critics: `CRITIC_LOG_INTERVAL`, `CRITIC_BEAT_INTERVAL` to tune chatter.
- Manager: `TEMPORAL_ADDRESS`, `TEMPORAL_NAMESPACE`, `TEMPORAL_TASK_QUEUE` to connect to Temporal.
- Telegram: `TELEGRAM_NOTIFY_URL=http://telegram-bot:8081/notify`, `TELEGRAM_CHAT_ID=<id>` for human alerts.
