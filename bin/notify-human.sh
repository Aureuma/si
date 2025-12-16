#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: notify-human.sh <message>" >&2
  echo "env: NOTIFY_URL (default http://localhost:8081/notify), TELEGRAM_CHAT_ID to override per-call." >&2
  exit 1
fi

MESSAGE="$*"
URL=${NOTIFY_URL:-http://localhost:8081/notify}
CHAT_ID_PAYLOAD=""
if [[ -n "${TELEGRAM_CHAT_ID:-}" ]]; then
  CHAT_ID_PAYLOAD=", \"chat_id\": ${TELEGRAM_CHAT_ID}"
fi

curl -fsSL -X POST -H "Content-Type: application/json" \
  -d "{\"message\": \"${MESSAGE//\"/\\\"}\"${CHAT_ID_PAYLOAD}}" \
  "$URL"
