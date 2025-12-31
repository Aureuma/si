# Postgres platform (CloudNativePG)

This directory installs a shared Postgres cluster in the `silexa-data` namespace, isolated from core Silexa services.

## Install
1) Install the operator (cluster-scoped CRDs + controller):

```bash
kubectl apply --server-side --force-conflicts -f infra/k8s/postgres/cnpg-operator.yaml
```

2) Create the admin secret (git-ignored env file):

```bash
cat > secrets/postgres-app-admin.env <<'ENV'
DB_ADMIN_USER=app_admin
DB_ADMIN_PASSWORD=<set-me>
DB_ADMIN_DB=app_admin
DB_ADMIN_HOST=silexa-postgres-rw.silexa-data.svc
DB_ADMIN_PORT=5432
ENV

source secrets/postgres-app-admin.env
kubectl -n silexa-data create secret generic silexa-postgres-app-admin \
  --from-literal=username="${DB_ADMIN_USER}" \
  --from-literal=password="${DB_ADMIN_PASSWORD}"
```
If you previously created the secret from `--from-env-file`, re-create it so `username`/`password` keys are present.

3) Apply the cluster + policies:

```bash
kubectl kustomize infra/k8s/postgres | kubectl apply -f -
```

4) Allow app namespaces to connect:

```bash
kubectl label ns <app-namespace> silexa.db-access=true
```

## Provision app databases

```bash
bin/app-db-shared.sh create <app>
```

## Notes
- The CNPG cluster creates `silexa-postgres-rw` and `silexa-postgres-ro` services in `silexa-data`.
- NetworkPolicy only allows ingress from namespaces labeled `silexa.db-access=true`.
