# Silexa Substrate

Silexa is an AI-first substrate for orchestrating multiple coding agents (Dyads). The primary control plane is Temporal on Kubernetes; Swarm files remain for local and legacy deployments.

## Layout
- `bootstrap.sh`: Host bootstrap for Ubuntu LTS (Docker, systemd, Node.js, git config).
- `infra/k8s/`: Kubernetes manifests for Temporal and core Silexa services.
- `docker-stack.yml`: Legacy Swarm stack for core services (manager + dyads + coder agent).
- `apps/`: Application repos built by agents (one repo per app).
- `agents/`: Agent-specific code and tooling.
- `coder`: Go-based agent container exposing `:8080/healthz` with docker/socket mounts for nested builds.
- `actor`: Node 22 base image for interactive CLI agents (install LLM tools like codex-cli inside the running container as needed).
- `critic`: Go watcher that reads actor container logs via the Docker socket and sends heartbeats to the manager.
- `manager`: Go service collecting critic heartbeats and storing state in Temporal.
- `manager-worker`: Temporal worker running the state workflow.
- `telegram-bot`: Go notifier listening on `:8081/notify` to push human-action items to Telegram (uses bot token secret + chat ID env).
- `bin/`: Helper scripts (e.g., `bin/coder-up.sh`).
- `bin/swarm-init.sh` / `bin/swarm-deploy.sh`: Initialize Swarm, build images, create secrets, and deploy the stack.
- `bin/swarm-secrets.sh`: Manage Docker secrets for Swarm services.
- `bin/app-db.sh`: Per-app Postgres lifecycle (create/drop/list/creds) with isolated containers and data dirs under `data/db-*`.

## Bootstrapping
Run on Ubuntu LTS as root or via sudo:

```bash
sudo /opt/silexa/bootstrap.sh
```

The script installs Docker CE (with buildx/compose plugins and Swarm), enables systemd services, sets git config to `SHi-ON <shawn@azdam.com>`, installs Node.js (Nodesource LTS, default 22.x), and initializes the git repo in `/opt/silexa`. After it completes, re-login so docker group membership takes effect.

Swarm deploy (legacy; for local use only):

```bash
cd /opt/silexa
bin/swarm-deploy.sh
```

## Dyads (Actor + Critic) and Manager
- Actors (`actor-web`, `actor-research`) are Node-based containers mounting `/opt/silexa/apps` and the docker socket; they run idle (`tail -f /dev/null`) so you can `docker exec -it` into them and drive interactive LLM CLIs (install inside the container as needed).
- Critics (`critic-web`, `critic-research`) run Go monitors that pull recent logs from their paired actor via the Docker socket and send periodic heartbeats to the manager.
- Manager (`manager`) listens on `:9090` and records heartbeats; fetch beats via `http://localhost:9090/beats` and liveness via `http://localhost:9090/healthz`.

Bring everything up (manager + two dyads + coder agent):

```bash
cd /opt/silexa
bin/swarm-deploy.sh
```

Check health and beats:

```bash
docker stack services silexa
docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'
curl -fsSL http://localhost:9090/beats
```

Open an interactive actor session (example for web dyad) and install your CLI tooling inside it:

```bash
bin/run-task.sh actor-web bash
# inside container: npm i -g <your-llm-cli>
```

The critics will mirror actor logs to their stdout and heartbeat to the manager; extend the critic to add richer policy/feedback loops as needed.

## Dynamic dyads (self-hosted spawning)
- Build base images once: `bin/build-images.sh`.
- Spawn a new dyad (actor+critic) on the shared network without editing the stack:

```bash
bin/spawn-dyad.sh <name> [role] [department]
# examples:
#   bin/spawn-dyad.sh marketing research marketing
#   bin/spawn-dyad.sh backend backend engineering
```

- Teardown a dyad: `bin/teardown-dyad.sh <name>`.
- Run commands in an actor: `bin/run-task.sh actor-<name> <command...>`.
- List running dyads (docker access required): `bin/list-dyads.sh`.

All dyads share `/opt/silexa/apps` and `/var/run/docker.sock`, so they can build new services or extend Silexa itself. Critics auto-heartbeat to the manager so the management layer can watch liveness and logs.

## Human-in-the-loop notifications (Telegram)
- Secrets: put the bot token into `secrets/telegram_bot_token` (file content is the raw token string). Do **not** commit secrets. Optionally set `TELEGRAM_CHAT_ID` in `.env` (see `.env.example`), or supply `chat_id` per message. Then run `bin/swarm-secrets.sh`.
- Start/refresh services: `bin/swarm-deploy.sh` (manager is pre-wired with `TELEGRAM_NOTIFY_URL=http://telegram-bot:8081/notify`).
- Command menu: `/status`, `/tasks`, `/task Title | command | notes`, `/help`. Any plain text message is logged as feedback; prefer `/task` for actionable asks.
- Send a notification from host: `TELEGRAM_CHAT_ID=<your_chat_id> bin/notify-human.sh "Codex login needed: run ssh -N -L 127.0.0.1:47123:ACTOR_IP:PORT user@host; then open http://127.0.0.1:47123/..."` 
- Create a structured human task (stored in manager and optionally forwarded to Telegram):  
  `TELEGRAM_CHAT_ID=<id> bin/add-human-task.sh "Codex login for actor web" "ssh -N -L 127.0.0.1:47123:ACTOR_IP:PORT user@host" "http://127.0.0.1:47123/..." "15m" "actor-web" "keep tunnel alive until callback"`  
  Then check `curl -fsSL http://localhost:9090/human-tasks`.
- Mark a task complete: `bin/complete-human-task.sh <id>`.
- Record the blocking step in `docs/human_queue.md` so humans have a durable checklist; critics/agents can also call `/notify` inside the cluster (URL `http://telegram-bot:8081/notify`). Payload supports `{ "message": "...", "chat_id": 123456789 }`.

## Codex CLI login flow (pattern)
1) Actor runs `npm i -g @openai/codex` then `codex login` (gets a local callback URL + port).
2) Actor writes a Human Action Queue item with the exact SSH tunnel command and URL; optionally triggers `notify-human.sh` so Telegram pings the operator.
3) Human runs the tunnel command on a browser-capable machine, opens the provided URL via the forwarded port, completes OAuth, and closes the tunnel.
4) Actor resumes once callback is received; mark the queue item as done.

## Secrets and RBAC notes
- Use docker secrets for tokens (e.g., `secrets/telegram_bot_token` mounted only into `telegram-bot`). Avoid leaking tokens via env except short-lived dev.
- Keep docker.sock access limited to containers that need it (actors, critics, coder-agent); notifier and manager do not mount it.
- Add new secrets by defining them under `secrets:` in `docker-stack.yml`, adding files under `secrets/`, and running `bin/swarm-secrets.sh`.

## Temporal and Kubernetes
See `docs/temporal-migration-plan.md` and `infra/k8s/` for the Kubernetes-first deployment path.

## Next steps
- Install Codex CLI (or another LLM-driven CLI) inside actor containers per task.
- Add more dyads via `bin/spawn-dyad.sh` or by extending `docker-stack.yml` and pointing `ACTOR_CONTAINER` accordingly.
- Wire higher-level task queues to feed work into actors and surface signals from manager to department heads.
