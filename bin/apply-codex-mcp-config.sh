#!/usr/bin/env bash
set -euo pipefail

# Copy the Codex MCP config into an actor/critic container's home.
# Usage: apply-codex-mcp-config.sh <dyad> [member]
# The config is taken from configs/codex-mcp-config.toml and copied to ~/.codex/config.toml

if [[ $# -lt 1 ]]; then
  echo "usage: apply-codex-mcp-config.sh <dyad> [member]" >&2
  exit 1
fi

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
CFG="${ROOT}/configs/codex-mcp-config.toml"
if [[ ! -f "$CFG" ]]; then
  echo "missing config template: $CFG" >&2
  exit 1
fi

DYAD="$1"
MEMBER="${2:-actor}"
DEST_DIR="${CODEX_CONFIG_DIR:-/root/.codex}"
DEST_FILE="${DEST_DIR}/config.toml"

# shellcheck source=bin/k8s-lib.sh
source "${ROOT}/bin/k8s-lib.sh"
POD=$("${ROOT}/bin/k8s-dyad-pod.sh" "$DYAD")

kube exec "$POD" -c "$MEMBER" -- mkdir -p "$DEST_DIR"
kube cp "$CFG" "${POD}:${DEST_FILE}" -c "$MEMBER"

echo "Applied Codex MCP config to ${DYAD}/${MEMBER}:${DEST_FILE}"
