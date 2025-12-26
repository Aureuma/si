#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: teardown-dyad.sh <name> [--purge]" >&2
  exit 1
fi

NAME="$1"
PURGE="false"
if [[ "${2:-}" == "--purge" ]]; then
  PURGE="true"
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

kube delete deployment "silexa-dyad-${NAME}" >/dev/null 2>&1 || true
echo "deleted deployment silexa-dyad-${NAME}"

if [[ "$PURGE" == "true" ]]; then
  kube delete pvc "codex-${NAME}" >/dev/null 2>&1 || true
  echo "deleted pvc codex-${NAME}"
fi
