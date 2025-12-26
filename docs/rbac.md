# RBAC Outline (lightweight)

## Roles
- `actor`: builds/tests code inside the dyad pod; needs access to `/opt/silexa/apps` PVC. No access to secrets beyond task-specific envs.
- `critic`: reads actor logs via the Kubernetes API; no code changes; can post feedback/human tasks; no secret mounting.
- `manager`: stores tasks/feedback; no privileged mounts; no secret mounts by default.
- `telegram-bot`: only needs the Telegram token secret and optional chat ID env.

## Departments
- Engineering: web, backend, infra dyads.
- Research: research dyad.
- Marketing: marketing dyad.

## Access policy (logical)
- Actors: read/write `apps/`, run image builds with an external builder; no access to secrets or manager state.
- Critics: read-only to pod logs; post to Manager APIs; no secrets.
- Manager: state stored in Temporal; may send notifications via `TELEGRAM_NOTIFY_URL`.
- Telegram-bot: read Telegram token secret; read-only chat ID env.

## Implementation notes
- Kubernetes service accounts and namespace-scoped RBAC live in `infra/k8s/silexa/rbac.yaml`.
- Secrets: `telegram-bot-token` mounted only into `telegram-bot`; other pods do not consume it.
- Volumes: Manager uses Temporal as the system of record (no local stateful volume).
- Labels/env: dyads carry `silexa.dyad`, `silexa.department`, `silexa.role` for identification.
