#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: recreate-dyad.sh <name> [role] [department]" >&2
  exit 1
fi

NAME="$1"
ROLE="${2:-infra}"
DEPT="${3:-$ROLE}"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

kube delete deployment "silexa-dyad-${NAME}" >/dev/null 2>&1 || true
echo "deleted deployment silexa-dyad-${NAME}"

"${ROOT_DIR}/bin/spawn-dyad.sh" "$NAME" "$ROLE" "$DEPT"
