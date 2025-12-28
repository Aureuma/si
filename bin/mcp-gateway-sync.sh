#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CATALOG_SRC="${CATALOG_SRC:-$ROOT/data/mcp-gateway/catalog.yaml}"
DEPLOYMENT="silexa-mcp-gateway"

# shellcheck source=bin/k8s-lib.sh
source "${ROOT}/bin/k8s-lib.sh"

if [[ ! -f "$CATALOG_SRC" ]]; then
  echo "catalog not found: $CATALOG_SRC" >&2
  exit 1
fi

POD=$(kube get pods -l app=silexa-mcp-gateway -o jsonpath='{.items[0].metadata.name}')
if [[ -z "$POD" ]]; then
  echo "mcp-gateway pod not found" >&2
  exit 1
fi

kube cp "$CATALOG_SRC" "${POD}:/catalog/catalog.yaml" -c mcp-gateway
kube rollout restart deployment "$DEPLOYMENT" >/dev/null

echo "synced catalog to ${POD} and restarted ${DEPLOYMENT}"
