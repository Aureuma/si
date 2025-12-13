# Silexa Substrate

Silexa is an AI-first substrate for orchestrating multiple coding agents. It lives at `/opt/silexa` on the host and uses Docker for isolation between app builds while allowing the core agent to run directly on the host.

## Layout
- `bootstrap.sh`: Host bootstrap for Ubuntu LTS (Docker, systemd, Node.js, git config).
- `docker-compose.yml`: Services for agents (currently a coder agent container).
- `apps/`: Application repos built by agents (one repo per app).
- `agents/`: Agent-specific code and tooling. `agents/coder` contains the Go-based coder agent container image.
- `bin/`: Helper binaries/scripts for local orchestration (e.g., `bin/coder-up.sh`).

## Bootstrapping
Run on Ubuntu LTS as root or via sudo:

```bash
sudo /opt/silexa/bootstrap.sh
```

The script installs Docker CE (with buildx/compose), enables systemd services, sets git config to `SHi-ON <shawn@azdam.com>`, installs Node.js (Nodesource LTS, default 22.x), and initializes the git repo in `/opt/silexa`. After it completes, re-login so docker group membership takes effect.

## Coder agent container
Build and start the coder agent container (uses Go 1.22, exposes `:8080/healthz`, mounts `apps/`, and has docker socket access for nested builds):

```bash
cd /opt/silexa
HOST_UID=$(id -u) HOST_GID=$(id -g) docker compose up -d coder-agent
# or
/opt/silexa/bin/coder-up.sh
```

Health check:

```bash
curl -fsSL http://localhost:8080/healthz
```

## Next steps
- Install Codex CLI on the host (requires Node.js) to drive the core agent.
- Add app repositories under `apps/` and start building with isolated Docker builds.
- Extend `agents/` with additional agent containers as workflows evolve.
