#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=bin/swarm-lib.sh
source "${ROOT_DIR}/bin/swarm-lib.sh"

STACK="$(swarm_stack_name)"
NETWORK="$(swarm_network_name)"

swarm_state=$(docker info --format '{{.Swarm.LocalNodeState}}' 2>/dev/null || true)
if [[ "$swarm_state" != "active" ]]; then
  echo "Initializing Docker Swarm..."
  docker swarm init >/dev/null
fi

node_id=$(docker info --format '{{.Swarm.NodeID}}')
if [[ -n "$node_id" ]]; then
  docker node update --label-add silexa.storage=local "$node_id" >/dev/null
fi

if ! docker network inspect "$NETWORK" >/dev/null 2>&1; then
  echo "Creating overlay network: $NETWORK"
  docker network create --driver overlay --attachable "$NETWORK" >/dev/null
fi

echo "Swarm ready (stack=${STACK}, network=${NETWORK})."
