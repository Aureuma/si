#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: app-secrets.sh <app-name>" >&2
  exit 1
fi

APP="$1"
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

ENV_FILE="${ROOT_DIR}/secrets/app-${APP}.env"
SECRET_NAME="app-${APP}-env"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "missing ${ENV_FILE}; create it with app env vars (DATABASE_URL, AUTH_SECRET, etc)" >&2
  exit 1
fi

kube create secret generic "$SECRET_NAME" \
  --from-env-file="$ENV_FILE" \
  --dry-run=client -o yaml | kube apply -f -

echo "applied secret ${SECRET_NAME} from ${ENV_FILE}"
