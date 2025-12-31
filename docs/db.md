## Shared Postgres platform (recommended)

Silexa runs a shared Postgres cluster in the `silexa-data` namespace (managed by CloudNativePG). Each app gets its own database and role inside that cluster.

### Quickstart
Provision a database for app `foo`:

```bash
bin/app-db-shared.sh create foo
bin/app-db-shared.sh creds foo
```

List or drop:

```bash
bin/app-db-shared.sh list
bin/app-db-shared.sh drop foo
```

### Credentials and access
- Admin credentials live in `secrets/postgres-app-admin.env` (git-ignored). These are used to create DBs/roles.
- App credentials are written to `secrets/db-<app>.env` (git-ignored) and also stored as a Kubernetes Secret named `db-<app>-credentials` in the app namespace.
- Default host is `silexa-postgres-rw.silexa-data.svc` (change with `SILEXA_DB_CLUSTER`/`SILEXA_DB_NAMESPACE`).
- NetworkPolicy only allows ingress from namespaces labeled `silexa.db-access=true`. Label app namespaces to opt in.

### Best practices
- One shared cluster with per-app databases keeps ops centralized and cost predictable.
- Keep migrations in each app repo (e.g., `apps/<app>/migrations`); dyads run migrations against the app DSN.
- Rotate app passwords by deleting `secrets/db-<app>.env`, then re-run `bin/app-db-shared.sh create <app>`.

## Legacy per-app Postgres (StatefulSet)

Each app can also run a dedicated Postgres Service + StatefulSet via `bin/app-db.sh`. This is still supported but not the preferred path for production.

```bash
bin/app-db.sh create foo
bin/app-db.sh creds foo
```
