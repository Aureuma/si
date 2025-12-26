#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: app-secrets.sh <app-name>" >&2
  exit 1
fi

APP="$1"
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
ENV_FILE="${ROOT_DIR}/secrets/app-${APP}.env"
SECRET_NAME="app-${APP}-env"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "missing ${ENV_FILE}; create it with app env vars (DATABASE_URL, AUTH_SECRET, etc)" >&2
  exit 1
fi

if docker secret inspect "$SECRET_NAME" >/dev/null 2>&1; then
  echo "secret ${SECRET_NAME} already exists"
  exit 0
fi

docker secret create "$SECRET_NAME" "$ENV_FILE" >/dev/null

echo "created secret ${SECRET_NAME} from ${ENV_FILE}"
