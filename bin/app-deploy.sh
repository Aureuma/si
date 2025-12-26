#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: app-deploy.sh <app-name> [--no-build] [--kustomize <path>]" >&2
  exit 1
fi

APP="$1"
shift

NO_BUILD=false
KUSTOMIZE_DIR=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-build) NO_BUILD=true ;;
    --kustomize)
      shift
      KUSTOMIZE_DIR="${1:-}"
      ;;
    *)
      echo "unknown flag: $1" >&2
      exit 1
      ;;
  esac
  shift || true
done

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

APP_DIR="${ROOT_DIR}/apps/${APP}"
if [[ -z "$KUSTOMIZE_DIR" ]]; then
  KUSTOMIZE_DIR="${APP_DIR}/infra/k8s"
fi

if [[ ! -d "$KUSTOMIZE_DIR" ]]; then
  echo "missing ${KUSTOMIZE_DIR}; create app k8s manifests first" >&2
  exit 1
fi

if [[ "$NO_BUILD" != "true" ]]; then
  "${ROOT_DIR}/bin/app-build.sh" "$APP"
fi

if [[ -f "${ROOT_DIR}/secrets/app-${APP}.env" ]]; then
  "${ROOT_DIR}/bin/app-secrets.sh" "$APP" || true
fi

kube apply -k "$KUSTOMIZE_DIR"

echo "App deployed via kustomize: ${KUSTOMIZE_DIR}"
