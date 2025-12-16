## Per-app Postgres pattern

Each web app gets its own Postgres container and data dir. Containers are isolated but share the `silexa_default` network so dyads can talk to them directly without host exposure.

### Quickstart
Create a database for app `foo`:

```bash
bin/app-db.sh create foo           # creates container silexa-db-foo + data dir data/db-foo
bin/app-db.sh creds foo            # show connection info
```

Optional host port exposure:

```bash
bin/app-db.sh create foo 55432     # binds localhost:55432 -> container 5432
```

List or drop:

```bash
bin/app-db.sh list                 # show running db containers
bin/app-db.sh drop foo             # stop container and remove data dir
bin/app-db.sh drop foo --keep-data # stop container but keep data
```

### Credentials and RBAC
- Credentials are written to `secrets/db-<app>.env` (git-ignored). Contents include `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `DB_HOST`, `DB_PORT`, and `DATABASE_URL`.
- Containers are named `silexa-db-<app>` on network `silexa_default`; connect from dyads using `DB_HOST=silexa-db-<app>`.
- No docker.sock or bot tokens are exposed to these DB containers.

### Best practices
- One database per app keeps blast radius small and enables per-app lifecycle (backup/restore/rotate).
- Avoid host port mapping unless you need local admin access; inside the cluster use the container hostname.
- Rotate passwords by editing `secrets/db-<app>.env` and recreating the container (`bin/app-db.sh drop <app>` then `create`).
- Keep schema migrations in each app repo (e.g., `apps/<app>/migrations`); dyads can run migrations against the app-specific DSN.***
