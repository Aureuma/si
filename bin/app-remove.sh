#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: app-remove.sh <app-name> [--kustomize <path>]" >&2
  exit 1
fi

APP="$1"
shift

KUSTOMIZE_DIR=""
while [[ $# -gt 0 ]]; do
  case "$1" in
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
  echo "missing ${KUSTOMIZE_DIR}" >&2
  exit 1
fi

kube delete -k "$KUSTOMIZE_DIR" --ignore-not-found

echo "App resources removed: ${KUSTOMIZE_DIR}"
