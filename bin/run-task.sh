#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: run-task.sh <actor-container> <command...>" >&2
  exit 1
fi

ACTOR="$1"
shift

docker exec -it "$ACTOR" "$@"
