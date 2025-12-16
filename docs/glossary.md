# Glossary

- **Actor**: Node-based container where interactive LLM CLI runs (e.g., Codex CLI). Mounts `/opt/silexa/apps` and docker.sock so it can build/run other services.
- **Critic**: Go container that tails its Actor's docker logs and heartbeats to the Manager, enabling oversight/feedback.
- **Dyad**: Paired Actor + Critic assigned to a domain (e.g., web, research). Managed together for work and monitoring.
- **Manager**: Go service at `:9090` that collects heartbeats from Critics (liveness/visibility). Extendable for richer signals.
- **Human Action Queue**: Append-only list (`docs/human_queue.md`) where agents record blocking human tasks (e.g., browser-based OAuth, hardware tokens) with exact commands and timeout windows. Humans clear items once resolved.
- **Telegram Bot**: Go control-plane service listening on `:8081/notify` to push human-queue items to Telegram. Uses `TELEGRAM_BOT_TOKEN` (from secret) and optionally `TELEGRAM_CHAT_ID` (env); callers can also supply `chat_id` per message.
- **Secrets handling**: Prefer docker secrets for tokens (e.g., `secrets/telegram_bot_token` mounted into containers). Environment variables are allowed for dev but should be avoided for long-lived credentials.
- **RBAC for secrets**: Only services that need a secret mount it (e.g., Telegram bot mounts its token; other containers do not). Avoid sharing docker socket to services that don’t require it; keep minimal permissions per container.
- **Human notification flow**: When an agent hits a browser-required step (e.g., `codex login`), it appends to the Human Action Queue and optionally calls the Telegram bot `/notify` endpoint to alert operators with the exact tunnel command and URL.
