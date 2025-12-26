#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STACK="${SILEXA_STACK:-silexa}"
export SILEXA_STACK="$STACK"
export SILEXA_NETWORK="${SILEXA_NETWORK:-silexa_net}"

"${ROOT_DIR}/bin/swarm-init.sh"
"${ROOT_DIR}/bin/build-images.sh"
"${ROOT_DIR}/bin/swarm-secrets.sh"

docker stack deploy -c "${ROOT_DIR}/docker-stack.yml" "$STACK"

echo "Stack deployed: $STACK"
