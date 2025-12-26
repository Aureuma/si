#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=bin/swarm-lib.sh
source "${ROOT_DIR}/bin/swarm-lib.sh"

STACK="$(swarm_stack_name)"

# Remove dyad services stuck at 0/N replicas.
stoplist=$(docker service ls --format '{{.Name}} {{.Replicas}}' | awk -v stack="$STACK" '$1 ~ "^" stack "_(actor|critic)-" && $2 ~ /^0\// {print $1}')
if [[ -n "$stoplist" ]]; then
  echo "$stoplist" | xargs docker service rm >/dev/null
  echo "Removed stopped dyad services"
fi

# Prune dangling images (quietly)
docker image prune -f >/dev/null && echo "Pruned dangling images"
