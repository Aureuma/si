#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: app-deploy.sh <app-name> [--no-build] [--stack-name <name>]" >&2
  exit 1
fi

APP="$1"
shift

NO_BUILD=false
STACK_NAME=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-build) NO_BUILD=true ;;
    --stack-name)
      shift
      STACK_NAME="${1:-}"
      ;;
    *)
      echo "unknown flag: $1" >&2
      exit 1
      ;;
  esac
  shift || true
done

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
APP_DIR="${ROOT_DIR}/apps/${APP}"
STACK_FILE="${APP_DIR}/infra/stack.yml"

if [[ ! -f "$STACK_FILE" ]]; then
  echo "missing ${STACK_FILE}; generate with bin/start-app-project.sh or create manually" >&2
  exit 1
fi

if [[ -z "$STACK_NAME" ]]; then
  STACK_NAME="silexa-app-${APP}"
fi

if [[ "$NO_BUILD" != "true" ]]; then
  "${ROOT_DIR}/bin/app-build.sh" "$APP"
fi

if [[ -f "${ROOT_DIR}/secrets/app-${APP}.env" ]]; then
  "${ROOT_DIR}/bin/app-secrets.sh" "$APP" || true
fi

export SILEXA_NETWORK="${SILEXA_NETWORK:-silexa_net}"

"${ROOT_DIR}/bin/swarm-init.sh"

if ! docker network inspect "$SILEXA_NETWORK" >/dev/null 2>&1; then
  echo "network ${SILEXA_NETWORK} not found; run bin/swarm-init.sh first" >&2
  exit 1
fi

APP_NAME="$APP" docker stack deploy -c "$STACK_FILE" "$STACK_NAME"

echo "App stack deployed: ${STACK_NAME}"
