#!/usr/bin/env bash
set -euo pipefail

# Build and start the MCP Gateway service.
# Usage: mcp-gateway-up.sh

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

echo "Building MCP Gateway image..."
(cd "$ROOT" && docker compose build mcp-gateway)

echo "Starting MCP Gateway..."
(cd "$ROOT" && docker compose up -d mcp-gateway)

echo "MCP Gateway running on port 8088 (streaming transport)."
echo "List capabilities via: docker compose run --rm mcp-gateway catalog ls"
