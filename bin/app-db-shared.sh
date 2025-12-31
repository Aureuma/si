#!/usr/bin/env bash
set -euo pipefail

# Provision per-app databases inside the shared Postgres cluster.
#
# Usage:
#   app-db-shared.sh create <app>
#   app-db-shared.sh drop <app> [--keep-creds]
#   app-db-shared.sh list
#   app-db-shared.sh creds <app>
#
# Env:
#   SILEXA_DB_NAMESPACE (default: silexa-data)
#   SILEXA_DB_CLUSTER (default: silexa-postgres)
#   SILEXA_APP_NAMESPACE (default: ${SILEXA_NAMESPACE:-silexa})
#   SILEXA_DB_ADMIN_ENV (default: secrets/postgres-app-admin.env)

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

DB_NAMESPACE=${SILEXA_DB_NAMESPACE:-silexa-data}
DB_CLUSTER=${SILEXA_DB_CLUSTER:-silexa-postgres}
APP_NAMESPACE=${SILEXA_APP_NAMESPACE:-$(k8s_namespace)}
ADMIN_ENV_FILE=${SILEXA_DB_ADMIN_ENV:-${ROOT_DIR}/secrets/postgres-app-admin.env}

usage() {
  echo "usage: app-db-shared.sh <create|drop|list|creds> ..." >&2
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

load_admin_env() {
  if [[ ! -f "$ADMIN_ENV_FILE" ]]; then
    echo "missing admin env file: ${ADMIN_ENV_FILE}" >&2
    exit 1
  fi
  # shellcheck disable=SC1090
  set -a
  source "$ADMIN_ENV_FILE"
  set +a
  if [[ -z "${DB_ADMIN_USER:-}" || -z "${DB_ADMIN_PASSWORD:-}" ]]; then
    echo "admin env file missing DB_ADMIN_USER/DB_ADMIN_PASSWORD" >&2
    exit 1
  fi
  DB_ADMIN_DB=${DB_ADMIN_DB:-app_admin}
  DB_ADMIN_PORT=${DB_ADMIN_PORT:-5432}
}

ensure_creds() {
  local app="$1"
  local pass_file="${ROOT_DIR}/secrets/db-${app}.env"
  if [[ -f "$pass_file" ]]; then
    return 0
  fi
  local db_pass
  db_pass=$(gen_pass)
  local host
  host="${DB_CLUSTER}-rw.${DB_NAMESPACE}.svc"
  {
    echo "DB_USER=${app}"
    echo "DB_PASSWORD=${db_pass}"
    echo "DB_NAME=${app}"
    echo "DB_HOST=${host}"
    echo "DB_PORT=5432"
    echo "DATABASE_URL=postgres://${app}:${db_pass}@${host}:5432/${app}?sslmode=disable"
  } > "$pass_file"
  echo "wrote credentials to ${pass_file}"
}

kube_db() {
  kubectl $(k8s_kubeconfig) -n "$DB_NAMESPACE" "$@"
}

kube_app() {
  kubectl $(k8s_kubeconfig) -n "$APP_NAMESPACE" "$@"
}

psql_exec() {
  local sql="$1"
  local pod
  pod=$(primary_pod)
  kube_db exec "$pod" -c postgres -- bash -lc \
    "PGPASSWORD='${DB_ADMIN_PASSWORD}' psql -h 127.0.0.1 -p ${DB_ADMIN_PORT} -U ${DB_ADMIN_USER} -d ${DB_ADMIN_DB} -v ON_ERROR_STOP=1 -c \"${sql}\""
}

create_app_db() {
  local app="$1"
  local pass_file="${ROOT_DIR}/secrets/db-${app}.env"
  if [[ ! -f "$pass_file" ]]; then
    echo "missing credentials file ${pass_file}" >&2
    exit 1
  fi
  # shellcheck disable=SC1090
  set -a
  source "$pass_file"
  set +a

  local role_exists
  role_exists=$(psql_check "SELECT 1 FROM pg_roles WHERE rolname='${app}'")
  if [[ "$role_exists" != "1" ]]; then
    psql_exec "CREATE ROLE \"${app}\" LOGIN PASSWORD '${DB_PASSWORD}'" "$app"
  fi

  local db_exists
  db_exists=$(psql_check "SELECT 1 FROM pg_database WHERE datname='${app}'")
  if [[ "$db_exists" != "1" ]]; then
    psql_exec "CREATE DATABASE \"${app}\" OWNER \"${app}\"" "$app"
  fi
}

psql_check() {
  local query="$1"
  local pod
  pod=$(primary_pod)
  kube_db exec "$pod" -c postgres -- bash -lc \
    "PGPASSWORD='${DB_ADMIN_PASSWORD}' psql -h 127.0.0.1 -p ${DB_ADMIN_PORT} -U ${DB_ADMIN_USER} -d ${DB_ADMIN_DB} -tAc \"${query}\"" | tr -d '[:space:]'
}

primary_pod() {
  local pod
  pod=$(kube_db get pods -l "cnpg.io/cluster=${DB_CLUSTER},cnpg.io/instanceRole=primary" \
    -o jsonpath='{.items[0].metadata.name}')
  if [[ -z "$pod" ]]; then
    echo "no primary pod found for cluster ${DB_CLUSTER} in namespace ${DB_NAMESPACE}" >&2
    exit 1
  fi
  echo "$pod"
}

case "${1:-}" in
  create)
    APP="${2:-}"
    require_arg "app" "$APP"
    load_admin_env
    ensure_creds "$APP"
    create_app_db "$APP"
    kube_app create secret generic "db-${APP}-credentials" \
      --from-env-file="${ROOT_DIR}/secrets/db-${APP}.env" \
      --dry-run=client -o yaml | kube_app apply -f -
    echo "Database ready for ${APP}."
    echo "Secret created in namespace ${APP_NAMESPACE}: db-${APP}-credentials"
    ;;
  drop)
    APP="${2:-}"
    require_arg "app" "$APP"
    KEEP_CREDS="false"
    if [[ "${3:-}" == "--keep-creds" ]]; then
      KEEP_CREDS="true"
    fi
    load_admin_env
    psql_exec "DROP DATABASE IF EXISTS \"${APP}\"" "$APP"
    psql_exec "DROP ROLE IF EXISTS \"${APP}\"" "$APP"
    kube_app delete secret "db-${APP}-credentials" --ignore-not-found
    if [[ "$KEEP_CREDS" != "true" ]]; then
      rm -f "${ROOT_DIR}/secrets/db-${APP}.env"
    fi
    ;;
  list)
    load_admin_env
    psql_exec "SELECT datname AS name, pg_get_userbyid(datdba) AS owner FROM pg_database WHERE datistemplate = false ORDER BY datname" list
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
