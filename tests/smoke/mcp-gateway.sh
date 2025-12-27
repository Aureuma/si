#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck source=bin/k8s-lib.sh
source "${ROOT}/bin/k8s-lib.sh"

POD=$(kube get pods -l app=silexa-mcp-gateway -o jsonpath='{.items[0].metadata.name}')
if [[ -z "$POD" ]]; then
  echo "mcp-gateway pod not found; deploy infra/k8s first" >&2
  exit 1
fi

kube exec "$POD" -c mcp-gateway -- /usr/local/bin/docker-mcp catalog ls || true
kube exec "$POD" -c mcp-gateway -- /usr/local/bin/docker-mcp catalog show docker-mcp --format yaml 2>/dev/null | head -n 20 || true
