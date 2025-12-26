#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: docker-target.sh <container-or-service>" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=bin/swarm-lib.sh
source "${ROOT_DIR}/bin/swarm-lib.sh"

resolve_container_id "$1"
