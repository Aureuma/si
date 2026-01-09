# Apps

Each app lives under `apps/<app>/` with metadata in `app.json`.

## Preferred layout (SvelteKit-first)
```
apps/<app>/
  web/        # SvelteKit (TypeScript)
  backend/    # optional Go service
  infra/      # IaC (Pulumi or similar)
  docs/       # plan.md, runbooks
  migrations/ # per-app DB migrations
  ui-tests/   # visual test targets
  app.json    # metadata + paths
```

If a legacy app uses a different layout, capture it in `app.json` under `paths`.

## Common commands
- Create a new app: `silexa app init <app>`
- Adopt an existing app: `silexa app adopt <app> --web-path <path> [--backend-path <path>]`
- List app metadata: `silexa app list`
- Build + deploy: `silexa app build <app>` then `silexa app deploy <app>`
- Check stack health: `silexa app status <app>`
