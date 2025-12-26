#!/usr/bin/env bash
set -euo pipefail

# Create/drop/list per-app Postgres deployments on Kubernetes.
#
# Usage:
#   app-db.sh create <app> [local_port]
#   app-db.sh drop <app> [--keep-data]
#   app-db.sh list
#   app-db.sh creds <app>
#
# Env:
#   POSTGRES_IMAGE (default: postgres:16-alpine)
#   DB_STORAGE_SIZE (default: 5Gi)
#   DB_STORAGE_CLASS (optional storageClassName)

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

IMAGE=${POSTGRES_IMAGE:-postgres:16-alpine}
DB_STORAGE_SIZE=${DB_STORAGE_SIZE:-5Gi}
DB_STORAGE_CLASS=${DB_STORAGE_CLASS:-}

usage() {
  echo "usage: app-db.sh <create|drop|list|creds> ..." >&2
  exit 1
}

require_arg() {
  if [[ -z "${2:-}" ]]; then
    echo "$1 required" >&2
    usage
  fi
}

gen_pass() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 16
  else
    head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n'
  fi
}

ensure_creds() {
  local app="$1"
  local pass_file="${ROOT_DIR}/secrets/db-${app}.env"
  if [[ -f "$pass_file" ]]; then
    return 0
  fi
  local db_pass
  db_pass=$(gen_pass)
  {
    echo "DB_USER=${app}"
    echo "DB_PASSWORD=${db_pass}"
    echo "DB_NAME=${app}"
    echo "DB_HOST=db-${app}"
    echo "DB_PORT=5432"
    echo "DATABASE_URL=postgres://${app}:${db_pass}@db-${app}:5432/${app}?sslmode=disable"
  } > "$pass_file"
  echo "wrote credentials to ${pass_file}"
}

apply_db_resources() {
  local app="$1"
  local storage_class_line=""
  if [[ -n "$DB_STORAGE_CLASS" ]]; then
    storage_class_line="storageClassName: ${DB_STORAGE_CLASS}"
  fi

  cat <<EOF | kube apply -f -
apiVersion: v1
kind: Service
metadata:
  name: db-${app}
  labels:
    app: db-${app}
    silexa.app: ${app}
    silexa.component: db
spec:
  ports:
    - name: postgres
      port: 5432
      targetPort: 5432
  selector:
    app: db-${app}
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: db-${app}
  labels:
    app: db-${app}
    silexa.app: ${app}
    silexa.component: db
spec:
  serviceName: db-${app}
  replicas: 1
  selector:
    matchLabels:
      app: db-${app}
  template:
    metadata:
      labels:
        app: db-${app}
        silexa.app: ${app}
        silexa.component: db
    spec:
      containers:
        - name: postgres
          image: ${IMAGE}
          ports:
            - containerPort: 5432
          envFrom:
            - secretRef:
                name: db-${app}-credentials
          volumeMounts:
            - name: data
              mountPath: /var/lib/postgresql/data
  volumeClaimTemplates:
    - metadata:
        name: data
        labels:
          app: db-${app}
          silexa.app: ${app}
          silexa.component: db
      spec:
        accessModes:
          - ReadWriteOnce
        resources:
          requests:
            storage: ${DB_STORAGE_SIZE}
        ${storage_class_line}
EOF
}

case "${1:-}" in
  create)
    APP="${2:-}"
    LOCAL_PORT="${3:-}"
    require_arg "app" "$APP"
    ensure_creds "$APP"
    kube create secret generic "db-${APP}-credentials" \
      --from-env-file="${ROOT_DIR}/secrets/db-${APP}.env" \
      --dry-run=client -o yaml | kube apply -f -
    apply_db_resources "$APP"
    echo "Postgres ready: service=db-${APP} (port 5432)"
    if [[ -n "$LOCAL_PORT" ]]; then
      echo "Port-forward: kubectl $(k8s_kubeconfig) -n $(k8s_namespace) port-forward svc/db-${APP} ${LOCAL_PORT}:5432"
    fi
    ;;
  drop)
    APP="${2:-}"
    require_arg "app" "$APP"
    KEEP_DATA="false"
    if [[ "${3:-}" == "--keep-data" ]]; then
      KEEP_DATA="true"
    fi
    kube delete statefulset "db-${APP}" --ignore-not-found
    kube delete service "db-${APP}" --ignore-not-found
    kube delete secret "db-${APP}-credentials" --ignore-not-found
    if [[ "$KEEP_DATA" != "true" ]]; then
      kube delete pvc -l "silexa.app=${APP},silexa.component=db" --ignore-not-found
    else
      echo "PVC kept for app ${APP}"
    fi
    ;;
  list)
    kube get statefulset -l silexa.component=db
    ;;
  creds)
    APP="${2:-}"
    require_arg "app" "$APP"
    PASS_FILE="${ROOT_DIR}/secrets/db-${APP}.env"
    if [[ ! -f "$PASS_FILE" ]]; then
      echo "No credentials file at ${PASS_FILE}" >&2
      exit 1
    fi
    cat "$PASS_FILE"
    ;;
  *)
    usage
    ;;
esac
