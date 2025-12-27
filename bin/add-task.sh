#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: add-task.sh <title> [kind] [priority] [description] [link] [notes] [complexity]" >&2
  echo "env: DYAD_TASK_COMPLEXITY (optional), MANAGER_URL (default http://localhost:9090), REQUESTED_BY (default router)" >&2
  exit 1
fi

TITLE="$1"
KIND="${2:-}"
PRIORITY="${3:-normal}"
DESC="${4:-}"
LINK="${5:-}"
NOTES="${6:-}"
COMPLEXITY="${7:-${DYAD_TASK_COMPLEXITY:-}}"
REQUESTED_BY="${REQUESTED_BY:-router}"
MANAGER_URL=${MANAGER_URL:-http://localhost:9090}

PAYLOAD=$(printf '{ "title":"%s","kind":"%s","priority":"%s","complexity":"%s","description":"%s","requested_by":"%s","notes":"%s","link":"%s" }' \
  "${TITLE//\"/\\\"}" \
  "${KIND//\"/\\\"}" \
  "${PRIORITY//\"/\\\"}" \
  "${COMPLEXITY//\"/\\\"}" \
  "${DESC//\"/\\\"}" \
  "${REQUESTED_BY//\"/\\\"}" \
  "${NOTES//\"/\\\"}" \
  "${LINK//\"/\\\"}" )

curl -fsSL -X POST -H "Content-Type: application/json" -d "$PAYLOAD" \
  "$MANAGER_URL/dyad-tasks"
