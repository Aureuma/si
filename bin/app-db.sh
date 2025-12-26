#!/usr/bin/env bash
set -euo pipefail

# Create/drop/list per-app Postgres services with isolated data directories.
# Each app gets its own service + data dir under ./data/db-<app>.
#
# Usage:
#   app-db.sh create <app> [host_port]
#   app-db.sh drop <app> [--keep-data]
#   app-db.sh list
#   app-db.sh creds <app>
#
# Env:
#   DOCKER_CMD (default: docker)
#   POSTGRES_IMAGE (default: postgres:16-alpine)
#   POSTGRES_NETWORK (default: from SILEXA_NETWORK or silexa_net)

DOCKER=${DOCKER_CMD:-docker}
IMAGE=${POSTGRES_IMAGE:-postgres:16-alpine}
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=bin/swarm-lib.sh
source "${ROOT_DIR}/bin/swarm-lib.sh"

NETWORK=${POSTGRES_NETWORK:-$(swarm_network_name)}
STACK="$(swarm_stack_name)"

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

case "${1:-}" in
  create)
    APP="${2:-}"
    HOST_PORT="${3:-}"
    require_arg "app" "$APP"
    SERVICE="${STACK}_db-${APP}"
    DATA_DIR="${ROOT_DIR}/data/db-${APP}"
    mkdir -p "$DATA_DIR"
    PASS_FILE="${ROOT_DIR}/secrets/db-${APP}.env"
    if [[ -f "$PASS_FILE" ]]; then
      # shellcheck disable=SC1090
      source "$PASS_FILE"
    else
      DB_PASSWORD=$(gen_pass)
      echo "DB_USER=${APP}" > "$PASS_FILE"
      echo "DB_PASSWORD=${DB_PASSWORD}" >> "$PASS_FILE"
      echo "DB_NAME=${APP}" >> "$PASS_FILE"
      echo "DB_HOST=${SERVICE}" >> "$PASS_FILE"
      echo "DB_PORT=5432" >> "$PASS_FILE"
      echo "DATABASE_URL=postgres://${APP}:${DB_PASSWORD}@${SERVICE}:5432/${APP}?sslmode=disable" >> "$PASS_FILE"
      echo "wrote credentials to ${PASS_FILE}"
    fi
    PUBLISH_ARG=()
    if [[ -n "$HOST_PORT" ]]; then
      PUBLISH_ARG=(--publish "${HOST_PORT}:5432")
    fi
    if $DOCKER service inspect "$SERVICE" >/dev/null 2>&1; then
      echo "Service ${SERVICE} already exists"
      exit 0
    fi
    echo "Starting Postgres for app=${APP} service=${SERVICE}"
    $DOCKER service create \
      --name "$SERVICE" \
      --network "$NETWORK" \
      --constraint node.labels.silexa.storage==local \
      --label "com.docker.stack.namespace=${STACK}" \
      --mount "type=bind,src=${DATA_DIR},dst=/var/lib/postgresql/data" \
      -e POSTGRES_USER="${DB_USER:-$APP}" \
      -e POSTGRES_PASSWORD="${DB_PASSWORD}" \
      -e POSTGRES_DB="${DB_NAME:-$APP}" \
      "${PUBLISH_ARG[@]}" \
      "$IMAGE" >/dev/null
    echo "Ready. Connect inside cluster: postgres://${DB_USER:-$APP}:${DB_PASSWORD}@${SERVICE}:5432/${DB_NAME:-$APP}"
    if [[ -n "$HOST_PORT" ]]; then
      echo "Host access: localhost:${HOST_PORT}"
    fi
    ;;
  drop)
    APP="${2:-}"
    require_arg "app" "$APP"
    KEEP_DATA="false"
    if [[ "${3:-}" == "--keep-data" ]]; then
      KEEP_DATA="true"
    fi
    SERVICE="${STACK}_db-${APP}"
    if $DOCKER service inspect "$SERVICE" >/dev/null 2>&1; then
      echo "Stopping ${SERVICE}"
      $DOCKER service rm "$SERVICE" >/dev/null
    else
      echo "Service ${SERVICE} not present"
    fi
    DATA_DIR="${ROOT_DIR}/data/db-${APP}"
    if [[ "$KEEP_DATA" != "true" && -d "$DATA_DIR" ]]; then
      echo "Removing data dir ${DATA_DIR}"
      if ! rm -rf "$DATA_DIR" 2>/dev/null; then
        sudo rm -rf "$DATA_DIR"
      fi
    else
      echo "Data kept at ${DATA_DIR}"
    fi
    ;;
  list)
    $DOCKER service ls --format 'table {{.Name}}\t{{.Replicas}}\t{{.Ports}}' | awk 'NR==1 || $1 ~ /_db-/'
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
