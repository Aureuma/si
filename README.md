# Silexa Substrate

Silexa is an AI-first substrate for orchestrating multiple coding agents (Dyads). The control plane is Temporal on Kubernetes.

## Layout
- `bootstrap.sh`: Host bootstrap for Ubuntu LTS (kubectl/helm, Node.js, git config).
- `infra/k8s/`: Kubernetes manifests for Temporal and core Silexa services.
- `apps/`: Application repos built by agents (one repo per app).
- `agents/`: Agent-specific code and tooling.
- `coder`: Go-based agent container exposing `:8080/healthz`.
- `actor`: Node 22 base image for interactive CLI agents (install LLM tools like codex-cli inside the running container as needed).
- `critic`: Go watcher that reads actor pod logs via the Kubernetes API and sends heartbeats to the manager.
- `manager`: Go service collecting critic heartbeats and storing state in Temporal.
- `manager-worker`: Temporal worker running the state workflow.
- `telegram-bot`: Go notifier listening on `:8081/notify` to push human-action items to Telegram (uses bot token secret + chat ID env).
- `bin/`: Helper scripts (e.g., `bin/coder-up.sh`).
- `bin/app-db.sh`: Per-app Postgres lifecycle (create/drop/list/creds) on Kubernetes.

## Bootstrapping
Run on Ubuntu LTS as root or via sudo:

```bash
sudo /opt/silexa/bootstrap.sh
```

The script installs kubectl + Helm, sets git config to `SHi-ON <shawn@azdam.com>`, installs Node.js (Nodesource LTS, default 22.x), and initializes the git repo in `/opt/silexa`. Install BuildKit (`buildctl` + `buildkitd`) if you plan to build images locally.

Core services deploy (Kubernetes):

```bash
cd /opt/silexa
kubectl apply -k infra/k8s/silexa
```

## Dyads (Actor + Critic) and Manager
- Actors are Node-based containers in a dyad pod; they run idle (`tail -f /dev/null`) so you can `kubectl exec` into them and drive interactive LLM CLIs.
- Critics run Go monitors that pull recent logs from their paired actor via the Kubernetes API and send periodic heartbeats to the manager.
- Manager (`manager`) listens on `:9090` and records heartbeats; fetch beats via `http://localhost:9090/beats` and liveness via `http://localhost:9090/healthz`.

Check health and beats:

```bash
kubectl get deployments -n silexa
curl -fsSL http://localhost:9090/beats
```

Open an interactive actor session (example for web dyad) and install your CLI tooling inside it:

```bash
bin/run-task.sh web bash
# inside container: npm i -g <your-llm-cli>
```

The critics will mirror actor logs to their stdout and heartbeat to the manager; extend the critic to add richer policy/feedback loops as needed.

## Dynamic dyads (self-hosted spawning)
- Build base images once: `bin/build-images.sh` (requires buildctl/buildkitd; push/load into your cluster registry).
- Spawn a new dyad (actor+critic) as a Kubernetes deployment:

```bash
bin/spawn-dyad.sh <name> [role] [department]
# examples:
#   bin/spawn-dyad.sh marketing research marketing
#   bin/spawn-dyad.sh backend backend engineering
```

- Teardown a dyad: `bin/teardown-dyad.sh <name>`.
- Run commands in an actor: `bin/run-task.sh <dyad> <command...>`.
- List running dyads: `bin/list-dyads.sh`.

All dyads share the repo workspace; critics auto-heartbeat to the manager so the management layer can watch liveness and logs.

## Human-in-the-loop notifications (Telegram)
- Secrets: put the bot token into `secrets/telegram_bot_token` (file content is the raw token string). Do **not** commit secrets. Optionally set `TELEGRAM_CHAT_ID` in `.env` (see `.env.example`), or supply `chat_id` per message. Then run `bin/rotate-telegram-token.sh <token>`.
- Start/refresh services: `kubectl apply -k infra/k8s/silexa` (manager is pre-wired with `TELEGRAM_NOTIFY_URL=http://silexa-telegram-bot:8081/notify`).
- Command menu: `/status`, `/tasks`, `/task Title | command | notes`, `/help`. Any plain text message is logged as feedback; prefer `/task` for actionable asks.
- Send a notification from host: `TELEGRAM_CHAT_ID=<your_chat_id> bin/notify-human.sh "Codex login needed: run ssh -N -L 127.0.0.1:47123:ACTOR_IP:PORT user@host; then open http://127.0.0.1:47123/..."` 
- Create a structured human task (stored in manager and optionally forwarded to Telegram):  
  `TELEGRAM_CHAT_ID=<id> bin/add-human-task.sh "Codex login for dyad web" "kubectl -n silexa port-forward pod/<pod> 47123:47124" "http://127.0.0.1:47123/..." "15m" "web" "keep port-forward alive until callback"`  
  Then check `curl -fsSL http://localhost:9090/human-tasks`.
- Mark a task complete: `bin/complete-human-task.sh <id>`.
- Record the blocking step in `docs/human_queue.md` so humans have a durable checklist; critics/agents can also call `/notify` inside the cluster (URL `http://telegram-bot:8081/notify`). Payload supports `{ "message": "...", "chat_id": 123456789 }`.

## Codex CLI login flow (pattern)
1) Actor runs `npm i -g @openai/codex` then `codex login` (gets a local callback URL + port).
2) Actor writes a Human Action Queue item with the exact `kubectl port-forward` command + URL; optionally triggers `notify-human.sh` so Telegram pings the operator.
3) Human runs the port-forward command on a browser-capable machine, opens the provided URL via the forwarded port, completes OAuth, and closes the port-forward.
4) Actor resumes once callback is received; mark the queue item as done.

## Secrets and RBAC notes
- Use Kubernetes secrets for tokens (e.g., `telegram-bot-token` mounted only into `telegram-bot`). Avoid leaking tokens via env except short-lived dev.
- Keep container privileges minimal; actors/critics no longer need a container runtime socket.
- Add new secrets via `kubectl create secret` (see `bin/rotate-telegram-token.sh` and docs).

## Temporal and Kubernetes
See `docs/temporal-migration-plan.md` and `infra/k8s/` for the Kubernetes-first deployment path.

## Next steps
- Install Codex CLI (or another LLM-driven CLI) inside actor containers per task.
- Add more dyads via `bin/spawn-dyad.sh`.
- Wire higher-level task queues to feed work into actors and surface signals from manager to department heads.
