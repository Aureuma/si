#!/usr/bin/env bash
set -euo pipefail

# Copy the Codex MCP config into an actor/critic container's home.
# Usage: apply-codex-mcp-config.sh <container-name>
# The config is taken from configs/codex-mcp-config.toml and copied to ~/.config/codex/config.toml

if [[ $# -lt 1 ]]; then
  echo "usage: apply-codex-mcp-config.sh <container-name>" >&2
  exit 1
fi

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
CFG="${ROOT}/configs/codex-mcp-config.toml"
if [[ ! -f "$CFG" ]]; then
  echo "missing config template: $CFG" >&2
  exit 1
fi

CONTAINER="$1"
DEST_DIR="/root/.config/codex"
DEST_FILE="${DEST_DIR}/config.toml"

docker exec "$CONTAINER" mkdir -p "$DEST_DIR"
docker cp "$CFG" "${CONTAINER}:${DEST_FILE}"

echo "Applied Codex MCP config to ${CONTAINER}:${DEST_FILE}"
