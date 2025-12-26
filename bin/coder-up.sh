#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=bin/swarm-lib.sh
source "${ROOT_DIR}/bin/swarm-lib.sh"

STACK="$(swarm_stack_name)"
SERVICE="${STACK}_coder-agent"

if docker service inspect "$SERVICE" >/dev/null 2>&1; then
  docker service update --replicas 1 "$SERVICE" >/dev/null
  echo "Coder agent ensured (service ${SERVICE})."
else
  echo "Coder agent service missing (${SERVICE}); deploy stack first." >&2
  exit 1
fi
