# RBAC Outline (lightweight)

## Roles
- `actor`: builds/tests code inside the dyad container; needs access to the repo workspace. No access to secrets beyond task-specific envs.
- `critic`: reads actor logs via Docker; no code changes; can post feedback/human tasks; no secret mounting.
- `manager`: stores tasks/feedback; no privileged mounts; no secret mounts by default.
- `telegram-bot`: only needs the Telegram token secret and optional chat ID env.

## Departments
- Engineering: web, backend, infra dyads.
- Research: research dyad.
- Marketing: marketing dyad.

## Access policy (logical)
- Actors: read/write `apps/`, run image builds with an external builder; no access to secrets or manager state.
- Critics: read-only to container logs; post to Manager APIs; no secrets.
- Manager: state stored in Temporal; may send notifications via `TELEGRAM_NOTIFY_URL`.
- Telegram-bot: read Telegram token secret; read-only chat ID env.

## Implementation notes
- Secrets: `telegram_bot_token` mounted only into `telegram-bot`; other containers do not consume it.
- Volumes: Manager uses Temporal as the system of record (no local stateful volume).
- Labels/env: dyads carry `silexa.dyad`, `silexa.department`, `silexa.role` for identification.
