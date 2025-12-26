#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: run-task.sh <actor-service> <command...>" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=bin/swarm-lib.sh
source "${ROOT_DIR}/bin/swarm-lib.sh"

TARGET="$1"
shift

CONTAINER_ID=$("${ROOT_DIR}/bin/docker-target.sh" "$TARGET")
docker exec -it "$CONTAINER_ID" "$@"
