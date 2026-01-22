# Silexa Substrate

Silexa is an AI-first substrate for orchestrating multiple coding agents (Dyads). The control plane is the Go manager with a local persisted state file, and the default runtime is vanilla Docker.

## Layout
- `apps/`: Application repos built by agents (one repo per app).
- `agents/`: Agent-specific code and tooling.
- `tools/silexa`: Go-based CLI for Docker + manager workflows.
- `actor`: Node base image for interactive CLI agents (install LLM tools inside the running container as needed).
- `critic`: Go watcher that reads actor logs via Docker and sends heartbeats to the manager.
- `manager`: Go service collecting critic heartbeats and storing state on disk.
- `telegram-bot`: Go notifier listening on `:8081/notify` to push human-action items to Telegram (uses bot token secret + chat ID env).

## Quickstart (Docker)
Build the CLI and images, then start the stack:

```bash
go run ./tools/silexa images build
go run ./tools/silexa stack up
```

Check health and beats:

```bash
curl -fsSL http://localhost:9090/healthz
curl -fsSL http://localhost:9090/beats
```

## Dyads (Actor + Critic)
- Spawn a dyad (actor+critic):

```bash
go run ./tools/silexa dyad spawn <name> [role] [department]
```

- Teardown a dyad:

```bash
go run ./tools/silexa dyad remove <name>
```

- Run commands in an actor:

```bash
go run ./tools/silexa dyad exec <dyad> --member actor -- bash
```

- List running dyads:

```bash
go run ./tools/silexa dyad list
```

All dyads share the repo workspace; critics auto-heartbeat to the manager so the management layer can watch liveness and logs.

## Human-in-the-loop notifications (Telegram)
- Secrets: put the bot token into `secrets/telegram_bot_token` (file content is the raw token string). Do **not** commit secrets. Optionally set `TELEGRAM_CHAT_ID` in `.env` (see `.env.example`), or supply `chat_id` per message.
- Start/refresh services: `go run ./tools/silexa stack up` (manager is pre-wired with `TELEGRAM_NOTIFY_URL=http://silexa-telegram-bot:8081/notify`).
- Command menu: `/status`, `/tasks`, `/task Title | command | notes`, `/help`. Any plain text message is logged as feedback; prefer `/task` for actionable asks.
- Send a notification from host:  
  `TELEGRAM_CHAT_ID=<id> go run ./tools/silexa notify "Codex login needed: open http://127.0.0.1:<port>/..."`  
- Create a structured human task (stored in manager and optionally forwarded to Telegram):  
  `TELEGRAM_CHAT_ID=<id> go run ./tools/silexa human add "Codex login for dyad web" "open http://127.0.0.1:<port>/" "http://127.0.0.1:<port>/" "15m" "web" "keep port open until callback"`  
  Then check `curl -fsSL http://localhost:9090/human-tasks`.
- Mark a task complete: `go run ./tools/silexa human complete <id>`.

## Codex CLI login flow (pattern)
1) Actor runs `npm i -g @openai/codex` then `codex login` (gets a local callback URL + port).
2) Actor writes a Human Action Queue item with the URL and/or port; optionally triggers `silexa notify` so Telegram pings the operator.
3) Human opens the URL on a browser-capable machine and completes OAuth.
4) Actor resumes once callback is received; mark the queue item as done.

## Secrets and access notes
- Tokens live in `secrets/` (mounted into the relevant containers).
- Keep container privileges minimal; actors/critics do not need a container runtime socket.

## Next steps
- Install Codex CLI (or another LLM-driven CLI) inside actor containers per task.
- Add more dyads via `silexa dyad spawn`.
- Wire higher-level task queues to feed work into actors and surface signals from manager to department heads.
