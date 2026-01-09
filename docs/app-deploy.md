# App deployment (Docker Compose)

Each app is deployed as its own Compose stack so it can be updated independently from core Silexa services.

## Prereqs
- Docker engine running.
- App metadata present: `apps/<app>/app.json`
- App compose file: `apps/<app>/infra/compose.yml`
- App env file: `secrets/app-<app>.env` (e.g., DATABASE_URL, AUTH_SECRET) or encrypted `secrets/app-<app>.env.sops` (see `docs/secrets.md`).

## Build + deploy
- Build images: `silexa app build <app>`
- Create/update env file: `silexa app secrets <app>`
- Deploy: `silexa app deploy <app>`

## Remove
- `silexa app remove <app>`

## Health
- `silexa app status <app>` shows container status for the app stack.

## Notes
- The SvelteKit template expects adapter-node output under `build/` and runs `node build`.
- The default template prefers pnpm; it will fall back to npm if an app has `package-lock.json`.
- For apps with custom Dockerfiles, place them at `apps/<app>/web/Dockerfile` or `apps/<app>/backend/Dockerfile` and `silexa app build` will use them.

Example `secrets/app-<app>.env`:
```
DATABASE_URL=postgres://...
AUTH_SECRET=...
PUBLIC_BASE_URL=https://...
```
