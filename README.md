# Silexa Substrate

Silexa is an AI-first substrate for orchestrating multiple coding agents (Dyads) on a single host. It lives at `/opt/silexa` on the host and uses Docker for isolation between app builds while allowing the core agent to run directly on the host.

## Layout
- `bootstrap.sh`: Host bootstrap for Ubuntu LTS (Docker, systemd, Node.js, git config).
- `docker-compose.yml`: Services for agents (manager + dyads + coder agent).
- `apps/`: Application repos built by agents (one repo per app).
- `agents/`: Agent-specific code and tooling.
- `coder`: Go-based agent container exposing `:8080/healthz` with docker/socket mounts for nested builds.
- `actor`: Node 22 base image for interactive CLI agents (install LLM tools like codex-cli inside the running container as needed).
- `critic`: Go watcher that reads actor container logs via the Docker socket and sends heartbeats to the manager.
- `manager`: Go service collecting critic heartbeats for monitoring.
- `telegram-bot`: Go notifier listening on `:8081/notify` to push human-action items to Telegram (uses bot token secret + chat ID env).
- `bin/`: Helper scripts (e.g., `bin/coder-up.sh`).

## Bootstrapping
Run on Ubuntu LTS as root or via sudo:

```bash
sudo /opt/silexa/bootstrap.sh
```

The script installs Docker CE (with buildx/compose), enables systemd services, sets git config to `SHi-ON <shawn@azdam.com>`, installs Node.js (Nodesource LTS, default 22.x), and initializes the git repo in `/opt/silexa`. After it completes, re-login so docker group membership takes effect.

## Dyads (Actor + Critic) and Manager
- Actors (`actor-web`, `actor-research`) are Node-based containers mounting `/opt/silexa/apps` and the docker socket; they run idle (`tail -f /dev/null`) so you can `docker exec -it` into them and drive interactive LLM CLIs (install inside the container as needed).
- Critics (`critic-web`, `critic-research`) run Go monitors that pull recent logs from their paired actor via the Docker socket and send periodic heartbeats to the manager.
- Manager (`manager`) listens on `:9090` and records heartbeats; fetch beats via `http://localhost:9090/beats`.

Bring everything up (manager + two dyads + coder agent):

```bash
cd /opt/silexa
HOST_UID=$(id -u) HOST_GID=$(id -g) docker compose up -d
```

Check health and beats:

```bash
docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'
curl -fsSL http://localhost:9090/beats
```

Open an interactive actor session (example for web dyad) and install your CLI tooling inside it:

```bash
docker exec -it silexa-actor-web bash
# inside container: npm i -g <your-llm-cli>
```

The critics will mirror actor logs to their stdout and heartbeat to the manager; extend the critic to add richer policy/feedback loops as needed.

## Dynamic dyads (self-hosted spawning)
- Build base images once: `bin/build-images.sh`.
- Spawn a new dyad (actor+critic) on the shared network without editing compose:

```bash
bin/spawn-dyad.sh <name> [role]
# example: bin/spawn-dyad.sh marketing research
```

- Teardown a dyad: `bin/teardown-dyad.sh <name>`.
- Run commands in an actor: `bin/run-task.sh silexa-actor-<name> <command...>`.

All dyads share `/opt/silexa/apps` and `/var/run/docker.sock`, so they can build new services or extend Silexa itself. Critics auto-heartbeat to the manager so the management layer can watch liveness and logs.

## Human-in-the-loop notifications (Telegram)
- Secrets: put the bot token into `secrets/telegram_bot_token` (file content is the raw token string). Do **not** commit secrets. Set `TELEGRAM_CHAT_ID` in `.env` (see `.env.example`).
- Start/refresh services: `HOST_UID=$(id -u) HOST_GID=$(id -g) docker compose up -d telegram-bot manager ...`
- Send a notification from host: `bin/notify-human.sh "Codex login needed: run ssh -N -L 127.0.0.1:47123:ACTOR_IP:PORT user@host; then open http://127.0.0.1:47123/..."`
- Record the blocking step in `docs/human_queue.md` so humans have a durable checklist; critics/agents can also call `/notify` inside the cluster (URL `http://telegram-bot:8081/notify`).

## Codex CLI login flow (pattern)
1) Actor runs `npm i -g @openai/codex` then `codex login` (gets a local callback URL + port).
2) Actor writes a Human Action Queue item with the exact SSH tunnel command and URL; optionally triggers `notify-human.sh` so Telegram pings the operator.
3) Human runs the tunnel command on a browser-capable machine, opens the provided URL via the forwarded port, completes OAuth, and closes the tunnel.
4) Actor resumes once callback is received; mark the queue item as done.

## Secrets and RBAC notes
- Use docker secrets for tokens (e.g., `secrets/telegram_bot_token` mounted only into `telegram-bot`). Avoid leaking tokens via env except short-lived dev.
- Keep docker.sock access limited to containers that need it (actors, critics, coder-agent); notifier and manager do not mount it.
- Add new secrets by defining them under `secrets:` in `docker-compose.yml` and wiring `*_FILE` env vars in the target service.

## Next steps
- Install Codex CLI (or another LLM-driven CLI) inside actor containers per task.
- Add more dyads by copying actor/critic service blocks in `docker-compose.yml` and pointing `ACTOR_CONTAINER` accordingly.
- Wire higher-level task queues to feed work into actors and surface signals from manager to department heads.
