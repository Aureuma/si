#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: recreate-dyad.sh <name> [role] [department]" >&2
  exit 1
fi

NAME="$1"
ROLE="${2:-infra}"
DEPT="${3:-$ROLE}"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=bin/swarm-lib.sh
source "${ROOT_DIR}/bin/swarm-lib.sh"

STACK="$(swarm_stack_name)"
ACTOR_SERVICE="${STACK}_actor-${NAME}"
CRITIC_SERVICE="${STACK}_critic-${NAME}"

docker service rm "$CRITIC_SERVICE" >/dev/null 2>&1 || true
docker service rm "$ACTOR_SERVICE" >/dev/null 2>&1 || true

"${ROOT_DIR}/bin/spawn-dyad.sh" "$NAME" "$ROLE" "$DEPT"
