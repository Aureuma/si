#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: add-human-task.sh <title> <commands> [url] [timeout] [requested_by] [notes]" >&2
  echo "env: MANAGER_URL (default http://localhost:9090), TELEGRAM_CHAT_ID (optional)" >&2
  exit 1
fi

TITLE="$1"
COMMANDS="$2"
URL_VAL="${3:-}"
TIMEOUT_VAL="${4:-}" # e.g., 15m
REQUESTED_BY="${5:-}"
NOTES="${6:-}"
CHAT_ID_JSON=""
if [[ -n "${TELEGRAM_CHAT_ID:-}" ]]; then
  CHAT_ID_JSON=", \"chat_id\": ${TELEGRAM_CHAT_ID}"
fi

MANAGER_URL=${MANAGER_URL:-http://localhost:9090}

PAYLOAD=$(printf '{ "title": "%s", "commands": "%s", "url": "%s", "timeout": "%s", "requested_by": "%s", "notes": "%s"%s }' \
  "${TITLE//\"/\\\"}" \
  "${COMMANDS//\"/\\\"}" \
  "${URL_VAL//\"/\\\"}" \
  "${TIMEOUT_VAL//\"/\\\"}" \
  "${REQUESTED_BY//\"/\\\"}" \
  "${NOTES//\"/\\\"}" \
  "${CHAT_ID_JSON}" )

curl -fsSL -X POST -H "Content-Type: application/json" \
  -d "$PAYLOAD" \
  "$MANAGER_URL/human-tasks"
