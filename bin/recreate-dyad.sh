#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: recreate-dyad.sh <name> [role] [department]" >&2
  exit 1
fi

NAME="$1"
ROLE="${2:-infra}"
DEPT="${3:-$ROLE}"

docker rm -f "silexa-critic-${NAME}" >/dev/null 2>&1 || true
docker rm -f "silexa-actor-${NAME}" >/dev/null 2>&1 || true

bin/spawn-dyad.sh "$NAME" "$ROLE" "$DEPT"

