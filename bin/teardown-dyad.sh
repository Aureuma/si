#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: teardown-dyad.sh <name>" >&2
  exit 1
fi

NAME="$1"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=bin/swarm-lib.sh
source "${ROOT_DIR}/bin/swarm-lib.sh"

STACK="$(swarm_stack_name)"
ACTOR_SERVICE="${STACK}_actor-${NAME}"
CRITIC_SERVICE="${STACK}_critic-${NAME}"

for svc in "$CRITIC_SERVICE" "$ACTOR_SERVICE"; do
  docker service rm "$svc" >/dev/null 2>&1 || true
  echo "removed service ${svc}"
done
