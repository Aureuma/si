# RBAC Outline (lightweight)

## Roles
- `actor`: builds/tests code inside containers; needs access to `/opt/silexa/apps` and docker.sock. No access to secrets beyond task-specific envs.
- `critic`: reads actor logs via docker.sock; no code changes; can post feedback/human tasks; no secret mounting.
- `manager`: stores tasks/feedback; no docker.sock; no secret mounts.
- `telegram-bot`: only needs telegram token secret and chat ID env; no docker.sock.

## Departments
- Engineering: web, backend, infra dyads.
- Research: research dyad.
- Marketing: marketing dyad.

## Access policy (logical)
- Actors: read/write `apps/`, run docker builds; no access to secrets or manager data volume.
- Critics: read-only to docker logs; post to Manager APIs; no secrets.
- Manager: read/write `data/manager`; no docker.sock; may send notifications via `TELEGRAM_NOTIFY_URL`.
- Telegram-bot: read telegram token secret; no docker.sock; read-only chat ID env.

## Implementation notes
- Compose mounts: only actors/critics/coder-agent get `/var/run/docker.sock`. Manager/telegram-bot do not.
- Secrets: `secrets/telegram_bot_token` mounted only into `telegram-bot`.
- Volumes: `./data/manager:/data` only for Manager.
- Labels/env: dyads carry `silexa.dyad`, `silexa.department`, `silexa.role` for identification.
