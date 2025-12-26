## Swarm retool plan (Silexa)

Legacy: this plan is superseded by `docs/temporal-migration-plan.md` (Temporal on Kubernetes).

This document assesses the current system and lays out a Swarm-first migration
plan that keeps complexity low, while preparing for a multi-node future.

### Assessment (pre-migration)
- **Compose-centric:** `docker-compose.yml` was the main control plane. Many scripts
  called `docker compose up` or `docker run` directly.
- **Container name coupling:** dyads were addressed by fixed container names
  (`silexa-actor-<dyad>`, `silexa-critic-<dyad>`). Several scripts and agents
  `docker exec` by name.
- **Host bind mounts:** repo + configs are bind-mounted into containers. This is
  fine on one node, but breaks if tasks reschedule to other nodes.
- **Secret handling:** only `telegram_bot_token` is a Compose secret; other tokens
  are plain env vars. Rotation is manual and host-dependent.
- **Operational scripts:** bootstrap, spawn, teardown, and reports are designed
  for container lifecycle, not Swarm service lifecycle.

### Goals
- Make Swarm the primary control plane (`docker stack deploy`).
- Use **Docker secrets** for sensitive tokens and keep secrets out of env when
  possible.
- Keep single-node deployment simple but support multi-node in the future.
- Minimize host dependencies (systemd, host-only paths) while acknowledging
  repo bind mounts must remain on the active node for now.

### Target architecture (Swarm-first)
- **Stack file:** `docker-stack.yml` defines all services (manager, router,
  brokers, codex-monitor, dyads, gateway, dashboard).
- **Swarm network:** dedicated overlay network for all services.
- **Secrets:** docker secrets for bot token and third-party tokens. Services read
  from `/run/secrets` where possible.
- **Configs:** keep `./configs` bind-mount for now (lowest complexity), document
  a future switch to Swarm configs per file.
- **Placement constraints:** pin stateful services to a labeled node
  (`silexa.storage=local`) so they are stable until shared storage is added.
- **Service naming:** use Swarm service names and resolve task container IDs for
  exec/log operations.

### Execution plan
1) **Create Swarm stack + bootstrap scripts**
   - Add `docker-stack.yml` (no `container_name`, use `deploy.resources`).
   - Add `bin/swarm-init.sh` to initialize Swarm and label the node.
   - Add `bin/swarm-deploy.sh` to build images and deploy stack.

2) **Update dyad lifecycle scripts**
   - Replace `docker run` with `docker service create` for dyads.
   - Update `spawn`, `teardown`, `list`, `run-task`, and `beam` scripts to
     resolve container IDs from service names (Swarm labels).

3) **Update agent code**
   - Critic + monitor: resolve service name -> container ID before Docker exec.
   - Program manager + router: emit Swarm service names in tasks/assignments.

4) **Docs + ops updates**
   - Update README and ops docs to use Swarm commands.
   - Document secrets lifecycle (`docker secret create/rm`) and rotation.

### Status (current)
- **Swarm stack**: `docker-stack.yml` added; Swarm bootstrap scripts live under `bin/swarm-*.sh`.
- **Dyad naming**: actor/critic identifiers standardized to `actor-<dyad>` / `critic-<dyad>` with service resolution via Swarm tasks.
- **Secrets**: Telegram/GH/Stripe handled via Docker secrets (`bin/swarm-secrets.sh`).
- **Scripts/docs**: updated to Swarm deploy and service naming.
- **Compose legacy**: old compose file archived at `docker-compose.legacy.yml`.

### Future (multi-node readiness)
- Move repo mounts to a shared volume (NFS, Ceph, or a CSI plugin).
- Use Swarm configs for `/configs` to avoid bind mounts.
- Add placement rules per dyad/role (e.g., web dyads on node pool A).
