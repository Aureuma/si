# App deployment (Swarm)

Each app is deployed as its own Swarm stack so it can be updated independently from core Silexa services.

## Prereqs
- Swarm initialized: `bin/swarm-init.sh`
- App metadata present: `apps/<app>/app.json`
- App stack file: `apps/<app>/infra/stack.yml`
- App env secret: `secrets/app-<app>.env` (e.g., DATABASE_URL, AUTH_SECRET)

## Build + deploy
- Build images: `bin/app-build.sh <app>`
- Create secret: `bin/app-secrets.sh <app>`
- Deploy stack: `bin/app-deploy.sh <app>`

Optional env overrides:
- `APP_WEB_PORT=3010 bin/app-deploy.sh <app>`
- `APP_BACKEND_PORT=8089 bin/app-deploy.sh <app>`

## Remove
- `bin/app-remove.sh <app>`

## Health
- `bin/app-status.sh <app>` checks service replicas for the app stack.

## Notes
- The SvelteKit template expects adapter-node output under `build/` and runs `node build`.
- The default template prefers pnpm; it will fall back to npm if an app has `package-lock.json`.
- For apps with custom Dockerfiles, place them at `apps/<app>/web/Dockerfile` or `apps/<app>/backend/Dockerfile` and `bin/app-build.sh` will use them.
- App stacks use the shared network `${SILEXA_NETWORK:-silexa_net}`.

Example `secrets/app-<app>.env`:
```
DATABASE_URL=postgres://...
AUTH_SECRET=...
PUBLIC_BASE_URL=https://...
```
