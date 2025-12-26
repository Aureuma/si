#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

failed=$(kube get pods -l app=silexa-dyad --field-selector=status.phase=Failed -o name || true)
if [[ -n "$failed" ]]; then
  echo "$failed" | xargs -n1 kube delete >/dev/null
  echo "Removed failed dyad pods"
else
  echo "No failed dyad pods"
fi
