# App deployment (Kubernetes)

Each app is deployed as its own kustomize bundle so it can be updated independently from core Silexa services.

## Prereqs
- Kubernetes cluster + namespace (default `silexa`).
- App metadata present: `apps/<app>/app.json`
- App k8s manifests: `apps/<app>/infra/k8s`
- App env secret: `secrets/app-<app>.env` (e.g., DATABASE_URL, AUTH_SECRET) or encrypted `secrets/app-<app>.env.sops` (see `docs/secrets.md`).

## Build + deploy
- Build images: `bin/app-build.sh <app>` (requires buildctl/buildkitd; push/load into your registry as needed)
- Local k3s import helper: `bin/image-build-import.sh -t <image:tag> -f <Dockerfile> <context>` (builds + imports into containerd)
- Create/update secret: `bin/app-secrets.sh <app>`
- Deploy: `bin/app-deploy.sh <app>`

## Remove
- `bin/app-remove.sh <app>`

## Health
- `bin/app-status.sh <app>` checks Deployment/StatefulSet readiness for the app label.

## Notes
- The SvelteKit template expects adapter-node output under `build/` and runs `node build`.
- The default template prefers pnpm; it will fall back to npm if an app has `package-lock.json`.
- For apps with custom Dockerfiles, place them at `apps/<app>/web/Dockerfile` or `apps/<app>/backend/Dockerfile` and `bin/app-build.sh` will use them.

Example `secrets/app-<app>.env`:
```
DATABASE_URL=postgres://...
AUTH_SECRET=...
PUBLIC_BASE_URL=https://...
```
