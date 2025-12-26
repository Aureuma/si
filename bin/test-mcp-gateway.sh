#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=bin/swarm-lib.sh
source "${ROOT}/bin/swarm-lib.sh"

NETWORK="$(swarm_network_name)"

docker build -t silexa/mcp-gateway:local "$ROOT/tools/mcp-gateway" >/dev/null

GH_TOKEN=""
if [[ -f "$ROOT/secrets/gh_token" ]]; then
  GH_TOKEN=$(cat "$ROOT/secrets/gh_token")
fi

STRIPE_API_KEY=""
if [[ -f "$ROOT/secrets/stripe_api_key" ]]; then
  STRIPE_API_KEY=$(cat "$ROOT/secrets/stripe_api_key")
fi

(cd "$ROOT" && docker run --rm \
  --network "$NETWORK" \
  -v "$ROOT/data/mcp-gateway:/catalog" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e GH_TOKEN="$GH_TOKEN" \
  -e STRIPE_API_KEY="$STRIPE_API_KEY" \
  silexa/mcp-gateway:local catalog ls || true)

(cd "$ROOT" && docker run --rm \
  --network "$NETWORK" \
  -v "$ROOT/data/mcp-gateway:/catalog" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e GH_TOKEN="$GH_TOKEN" \
  -e STRIPE_API_KEY="$STRIPE_API_KEY" \
  silexa/mcp-gateway:local catalog show docker-mcp --format yaml) | head -n 20 || true
