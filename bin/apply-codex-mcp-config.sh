#!/usr/bin/env bash
set -euo pipefail

# Copy the Codex MCP config into an actor/critic container's home.
# Usage: apply-codex-mcp-config.sh <container-or-service>
# The config is taken from configs/codex-mcp-config.toml and copied to ~/.config/codex/config.toml

if [[ $# -lt 1 ]]; then
  echo "usage: apply-codex-mcp-config.sh <container-or-service>" >&2
  exit 1
fi

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
CFG="${ROOT}/configs/codex-mcp-config.toml"
if [[ ! -f "$CFG" ]]; then
  echo "missing config template: $CFG" >&2
  exit 1
fi

TARGET="$1"
DEST_DIR="/root/.config/codex"
DEST_FILE="${DEST_DIR}/config.toml"

CONTAINER_ID=$("${ROOT}/bin/docker-target.sh" "$TARGET")

docker exec "$CONTAINER_ID" mkdir -p "$DEST_DIR"
docker cp "$CFG" "${CONTAINER_ID}:${DEST_FILE}"

echo "Applied Codex MCP config to ${TARGET}:${DEST_FILE}"
