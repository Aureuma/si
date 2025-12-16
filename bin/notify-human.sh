#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: notify-human.sh <message>" >&2
  exit 1
fi

MESSAGE="$*"
URL=${NOTIFY_URL:-http://localhost:8081/notify}

curl -fsSL -X POST -H "Content-Type: application/json" \
  -d "{\"message\": \"${MESSAGE//\"/\\\"}\"}" \
  "$URL"
