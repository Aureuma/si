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
ENC_FILE=""
for candidate in "${ENV_FILE}.sops" "${ENV_FILE}.sops.env" "${ENV_FILE}.env.sops"; do
  if [[ -f "$candidate" ]]; then
    ENC_FILE="$candidate"
    break
  fi
done
SECRET_NAME="app-${APP}-env"

if [[ ! -f "$ENV_FILE" ]]; then
  if [[ -z "$ENC_FILE" ]]; then
    echo "missing ${ENV_FILE}; create it or provide an encrypted ${ENV_FILE}.sops file" >&2
    exit 1
  fi
  if ! command -v sops >/dev/null 2>&1; then
    echo "sops is required to decrypt ${ENC_FILE}" >&2
    exit 1
  fi
  tmp=$(mktemp)
  cleanup() { rm -f "$tmp"; }
  trap cleanup EXIT
  sops -d "$ENC_FILE" > "$tmp"
  ENV_FILE="$tmp"
fi

kube create secret generic "$SECRET_NAME" \
  --from-env-file="$ENV_FILE" \
  --dry-run=client -o yaml | kube apply -f -

echo "applied secret ${SECRET_NAME} from ${ENV_FILE}"
