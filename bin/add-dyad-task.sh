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

PAYLOAD=$(python3 - <<'PY' "$TITLE" "$KIND" "$DESC" "$DYAD" "$ACTOR" "$CRITIC" "$PRIORITY" "$COMPLEXITY" "$REQUESTED_BY" "$NOTES" "$LINK"
import json
import sys

title, kind, desc, dyad, actor, critic, priority, complexity, requested_by, notes, link = sys.argv[1:]
payload = {
    "title": title,
    "kind": kind,
    "description": desc,
    "dyad": dyad,
    "actor": actor,
    "critic": critic,
    "priority": priority,
    "complexity": complexity,
    "requested_by": requested_by,
    "notes": notes,
    "link": link,
}
print(json.dumps(payload))
PY
)

curl -fsSL -X POST -H "Content-Type: application/json" -d "$PAYLOAD" \
  "$MANAGER_URL/dyad-tasks"
