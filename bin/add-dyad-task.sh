#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: add-dyad-task.sh <title> <dyad> [actor] [critic] [priority] [description] [link] [notes] [complexity]" >&2
  echo "env: DYAD_TASK_KIND (optional), DYAD_TASK_COMPLEXITY (optional), MANAGER_URL (default http://localhost:9090)" >&2
  exit 1
fi

TITLE="$1"
DYAD="$2"
ACTOR="${3:-}"
CRITIC="${4:-}"
PRIORITY="${5:-normal}"
DESC="${6:-}"
LINK="${7:-}"
NOTES="${8:-}"
COMPLEXITY="${9:-${DYAD_TASK_COMPLEXITY:-}}"
KIND="${DYAD_TASK_KIND:-}"
REQUESTED_BY="${REQUESTED_BY:-router}"
MANAGER_URL=${MANAGER_URL:-http://localhost:9090}

PAYLOAD=$(printf '{ "title":"%s","kind":"%s","description":"%s","dyad":"%s","actor":"%s","critic":"%s","priority":"%s","complexity":"%s","requested_by":"%s","notes":"%s","link":"%s" }' \
  "${TITLE//\"/\\\"}" \
  "${KIND//\"/\\\"}" \
  "${DESC//\"/\\\"}" \
  "${DYAD//\"/\\\"}" \
  "${ACTOR//\"/\\\"}" \
  "${CRITIC//\"/\\\"}" \
  "${PRIORITY//\"/\\\"}" \
  "${COMPLEXITY//\"/\\\"}" \
  "${REQUESTED_BY//\"/\\\"}" \
  "${NOTES//\"/\\\"}" \
  "${LINK//\"/\\\"}" )

curl -fsSL -X POST -H "Content-Type: application/json" -d "$PAYLOAD" \
  "$MANAGER_URL/dyad-tasks"
