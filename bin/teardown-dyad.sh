#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: teardown-dyad.sh <name>" >&2
  exit 1
fi

NAME="$1"
ACTOR="silexa-actor-${NAME}"
CRITIC="silexa-critic-${NAME}"

docker rm -f "$CRITIC" >/dev/null 2>&1 || true
docker rm -f "$ACTOR" >/dev/null 2>&1 || true
echo "dyad ${NAME} removed"
