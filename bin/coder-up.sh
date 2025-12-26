#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

if kube get deployment silexa-coder-agent >/dev/null 2>&1; then
  kube scale deployment silexa-coder-agent --replicas 1 >/dev/null
  echo "Coder agent ensured (deployment silexa-coder-agent)."
else
  echo "Coder agent deployment missing; apply infra/k8s first." >&2
  exit 1
fi
