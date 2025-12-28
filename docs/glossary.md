# Glossary

- **Actor**: Node-based container where interactive LLM CLI runs (e.g., Codex CLI). Runs inside a dyad pod with `/opt/silexa/apps` mounted from PVC; no Docker socket is required.
- **Critic**: Go container that tails its Actor's pod logs via the Kubernetes API and heartbeats to the Manager, enabling oversight/feedback.
- **Dyad**: Paired Actor + Critic assigned to a domain (e.g., web, research). Managed together for work and monitoring.
- **Dyad Registry**: Manager-backed list of dyads and their current status. API `/dyads` (GET list with heartbeat state, POST upsert metadata).
- **Department**: Logical grouping (e.g., engineering, marketing) assigned to a Dyad via `spawn-dyad.sh [--temporal] <name> <role> <department>`; exported to containers via `DEPARTMENT` env.
- **Manager**: Go service at `:9090` that collects heartbeats from Critics (liveness/visibility). Extendable for richer signals.
- **Human Action Queue**: Append-only list (`docs/human_queue.md`) where agents record blocking human tasks (e.g., browser-based OAuth, hardware tokens) with exact commands and timeout windows. Humans clear items once resolved.
- **Telegram Bot**: Go control-plane service listening on `:8081/notify` to push human-queue items to Telegram. Uses `TELEGRAM_BOT_TOKEN` (from secret) and optionally `TELEGRAM_CHAT_ID` (env); callers can also supply `chat_id` per message.
- **Manager human tasks API**: `/human-tasks` (GET list, POST create `{title,commands,url,timeout,requested_by,notes,chat_id?}`) and `/human-tasks/complete?id=N`. On create, manager optionally forwards a message to the Telegram Bot (`TELEGRAM_NOTIFY_URL`/`TELEGRAM_CHAT_ID`).
- **Secrets handling**: Prefer Kubernetes secrets for tokens (e.g., `telegram-bot-token` mounted into the bot pod). Environment variables are allowed for dev but should be avoided for long-lived credentials.
- **RBAC for secrets**: Only services that need a secret mount it (e.g., Telegram bot mounts its token; other containers do not). Use namespace-scoped RBAC and least-privilege service accounts; avoid sharing privileged mounts.
- **Human notification flow**: When an agent hits a browser-required step (e.g., `codex login`), it appends to the Human Action Queue and optionally calls the Telegram bot `/notify` endpoint to alert operators with the exact tunnel command and URL.
- **Human tasks helper scripts**: `bin/add-human-task.sh` to create tasks (optionally Telegram), `bin/complete-human-task.sh` to close tasks, `bin/notify-human.sh` for ad-hoc messages.
- **Beam**: A repeatable, registered human-in-the-loop runbook captured as a named flow. In a Beam, the agent runs the entire automation end-to-end, then sends the human only the exact action(s) to execute (typically an SSH tunnel command and/or a URL). The agent stays “in beam” until the human completes the step and the agent can verify success (e.g., `codex login status`). Examples: `codex_login` Beam, credential rotation Beam.
- **Dyad Task Board**: Manager-backed task board for dyads (actors/critics). API `/dyad-tasks` (list/create) and `/dyad-tasks/update` (status/assignment). Used by router to allocate work; dyads update status (`todo`, `in_progress`, `review`, `blocked`, `done`). Notifications go to Telegram on create/update.
