#!/usr/bin/env bash
set -euo pipefail

# Build and start the MCP Gateway service.
# Usage: mcp-gateway-up.sh

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=bin/k8s-lib.sh
source "${ROOT}/bin/k8s-lib.sh"

DEPLOYMENT="silexa-mcp-gateway"
IMAGE="${MCP_GATEWAY_IMAGE:-silexa/mcp-gateway:local}"

echo "Building MCP Gateway image: ${IMAGE}"
"${ROOT}/bin/image-build.sh" -t "$IMAGE" "$ROOT/tools/mcp-gateway"

if kube get deployment "$DEPLOYMENT" >/dev/null 2>&1; then
  echo "Updating MCP Gateway deployment..."
  kube set image deployment "$DEPLOYMENT" mcp-gateway="$IMAGE" >/dev/null
  kube rollout restart deployment "$DEPLOYMENT" >/dev/null
else
  echo "MCP Gateway deployment missing (${DEPLOYMENT}); apply infra/k8s first." >&2
  exit 1
fi

echo "MCP Gateway running on port 8088 (streaming transport)."
