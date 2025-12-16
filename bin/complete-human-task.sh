#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: complete-human-task.sh <id>" >&2
  echo "env: MANAGER_URL (default http://localhost:9090)" >&2
  exit 1
fi

ID="$1"
MANAGER_URL=${MANAGER_URL:-http://localhost:9090}

curl -fsSL -X POST "$MANAGER_URL/human-tasks/complete?id=${ID}"
