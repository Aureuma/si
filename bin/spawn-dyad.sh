#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: spawn-dyad.sh <name> [role] [department]" >&2
  exit 1
fi

NAME="$1"
ROLE="${2:-generic}"
DEPT="${3:-$ROLE}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NETWORK="silexa_default"
MANAGER_URL="${MANAGER_URL:-http://silexa-manager:9090}"

# Ensure shared network exists
if ! docker network inspect "$NETWORK" >/dev/null 2>&1; then
  docker network create "$NETWORK" >/dev/null
fi

# Start actor
if docker ps -a --format '{{.Names}}' | grep -q "^silexa-actor-${NAME}$"; then
  echo "actor silexa-actor-${NAME} already exists"
else
  docker run -d --name "silexa-actor-${NAME}" \
    --network "$NETWORK" \
    --restart unless-stopped \
    --workdir /workspace/apps \
    -v "$ROOT_DIR/apps:/workspace/apps" \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -e ROLE="$ROLE" \
    -e DEPARTMENT="$DEPT" \
    silexa/actor:local tail -f /dev/null
fi

# Start critic
if docker ps -a --format '{{.Names}}' | grep -q "^silexa-critic-${NAME}$"; then
  echo "critic silexa-critic-${NAME} already exists"
else
  docker run -d --name "silexa-critic-${NAME}" \
    --network "$NETWORK" \
    --restart unless-stopped \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -e ACTOR_CONTAINER="silexa-actor-${NAME}" \
    -e MANAGER_URL="$MANAGER_URL" \
    -e DEPARTMENT="$DEPT" \
    -e ROLE="$ROLE" \
    silexa/critic:local
fi

echo "dyad ${NAME} ready: actor=silexa-actor-${NAME}, critic=silexa-critic-${NAME}"
