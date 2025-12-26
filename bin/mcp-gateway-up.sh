#!/usr/bin/env bash
set -euo pipefail

# Build and start the MCP Gateway service.
# Usage: mcp-gateway-up.sh

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=bin/swarm-lib.sh
source "${ROOT}/bin/swarm-lib.sh"

STACK="$(swarm_stack_name)"
SERVICE="${STACK}_mcp-gateway"

echo "Building MCP Gateway image..."
docker build -t silexa/mcp-gateway:local "$ROOT/tools/mcp-gateway"

if docker service inspect "$SERVICE" >/dev/null 2>&1; then
  echo "Updating MCP Gateway service..."
  docker service update --image silexa/mcp-gateway:local --force "$SERVICE" >/dev/null
else
  echo "MCP Gateway service missing (${SERVICE}); deploy stack first." >&2
  exit 1
fi

echo "MCP Gateway running on port 8088 (streaming transport)."
