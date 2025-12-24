#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: update-dyad-task.sh <id> <status> [notes] [actor] [critic]" >&2
  echo "status: todo|in_progress|review|blocked|done" >&2
  echo "env: MANAGER_URL (default http://localhost:9090)" >&2
  exit 1
fi

ID="$1"
STATUS="$2"
NOTES="${3:-}"
ACTOR="${4:-}"
CRITIC="${5:-}"
MANAGER_URL=${MANAGER_URL:-http://localhost:9090}

PAYLOAD=$(printf '{ "id":%s,"status":"%s","notes":"%s","actor":"%s","critic":"%s" }' \
  "$ID" \
  "${STATUS//\"/\\\"}" \
  "${NOTES//\"/\\\"}" \
  "${ACTOR//\"/\\\"}" \
  "${CRITIC//\"/\\\"}")

curl -fsSL -X POST -H "Content-Type: application/json" -d "$PAYLOAD" \
  "$MANAGER_URL/dyad-tasks/update"
