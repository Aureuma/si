#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
MANAGER_URL=${MANAGER_URL:-http://localhost:9090}
MANAGER_URL=${MANAGER_URL%/}

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required" >&2
  exit 1
fi
if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 is required" >&2
  exit 1
fi

wait_for_manager() {
  local i
  for i in $(seq 1 30); do
    if curl -fsSL "${MANAGER_URL}/healthz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "manager not reachable at ${MANAGER_URL}" >&2
  exit 1
}

http_post() {
  local path="$1"
  local payload="$2"
  local out="$3"
  curl -sS -o "$out" -w "%{http_code}" -H "Content-Type: application/json" \
    -d "$payload" "${MANAGER_URL}${path}"
}

RUN_ID=$(date -u +%Y%m%d%H%M%S)
DYAD_NAME="assign-${RUN_ID}"
UNKNOWN_DYAD="unknown-${RUN_ID}"
ROLE=${DYAD_ROLE:-qa}
DEPT=${DYAD_DEPT:-qa}

wait_for_manager

env MANAGER_URL="$MANAGER_URL" "${ROOT_DIR}/bin/register-dyad.sh" "$DYAD_NAME" "$ROLE" "$DEPT" >/dev/null

TMP_DIR=$(mktemp -d)
cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

task_ok="${TMP_DIR}/task_ok.json"
task_unknown="${TMP_DIR}/task_unknown.json"
task_unassigned_ip="${TMP_DIR}/task_unassigned_ip.json"
update_no_dyad="${TMP_DIR}/update_no_dyad.json"
update_ok="${TMP_DIR}/update_ok.json"

payload_ok=$(python3 - <<'PY' "$RUN_ID"
import json, sys
run_id = sys.argv[1]
print(json.dumps({
    "title": f"Assignment enforcement ok {run_id}",
    "description": "unassigned todo task",
    "status": "todo",
    "requested_by": "test",
}))
PY
)
status=$(http_post "/dyad-tasks" "$payload_ok" "$task_ok")
if [[ "$status" != "200" ]]; then
  echo "expected 200 creating unassigned todo, got $status" >&2
  cat "$task_ok" >&2
  exit 1
fi

TASK_ID=$(python3 - <<'PY' "$task_ok"
import json, sys
path = sys.argv[1]
with open(path, "r", encoding="utf-8") as fh:
    data = json.load(fh)
task_id = data.get("id")
if not task_id:
    print("missing task id")
    sys.exit(1)
if data.get("status") != "todo":
    print("expected status todo")
    sys.exit(1)
if data.get("dyad"):
    print("expected empty dyad for unassigned task")
    sys.exit(1)
print(task_id)
PY
)

payload_unknown=$(python3 - <<'PY' "$RUN_ID" "$UNKNOWN_DYAD"
import json, sys
run_id, dyad = sys.argv[1:3]
print(json.dumps({
    "title": f"Assignment enforcement unknown {run_id}",
    "status": "todo",
    "dyad": dyad,
    "requested_by": "test",
}))
PY
)
status=$(http_post "/dyad-tasks" "$payload_unknown" "$task_unknown")
if [[ "$status" != "409" ]]; then
  echo "expected 409 for unregistered dyad, got $status" >&2
  cat "$task_unknown" >&2
  exit 1
fi

payload_unassigned_ip=$(python3 - <<'PY' "$RUN_ID"
import json, sys
run_id = sys.argv[1]
print(json.dumps({
    "title": f"Assignment enforcement in-progress {run_id}",
    "status": "in_progress",
    "requested_by": "test",
}))
PY
)
status=$(http_post "/dyad-tasks" "$payload_unassigned_ip" "$task_unassigned_ip")
if [[ "$status" != "409" ]]; then
  echo "expected 409 for in_progress without dyad, got $status" >&2
  cat "$task_unassigned_ip" >&2
  exit 1
fi

payload_update_no_dyad=$(python3 - <<'PY' "$TASK_ID"
import json, sys
task_id = int(sys.argv[1])
print(json.dumps({
    "id": task_id,
    "status": "in_progress",
}))
PY
)
status=$(http_post "/dyad-tasks/update" "$payload_update_no_dyad" "$update_no_dyad")
if [[ "$status" != "409" ]]; then
  echo "expected 409 for update without dyad, got $status" >&2
  cat "$update_no_dyad" >&2
  exit 1
fi

payload_update_ok=$(python3 - <<'PY' "$TASK_ID" "$DYAD_NAME"
import json, sys
task_id = int(sys.argv[1])
dyad = sys.argv[2]
print(json.dumps({
    "id": task_id,
    "status": "in_progress",
    "dyad": dyad,
    "actor": "actor",
    "critic": "critic",
}))
PY
)
status=$(http_post "/dyad-tasks/update" "$payload_update_ok" "$update_ok")
if [[ "$status" != "200" ]]; then
  echo "expected 200 for update with dyad, got $status" >&2
  cat "$update_ok" >&2
  exit 1
fi

python3 - <<'PY' "$update_ok" "$DYAD_NAME"
import json, sys
path, dyad = sys.argv[1:3]
with open(path, "r", encoding="utf-8") as fh:
    data = json.load(fh)
if data.get("status") != "in_progress":
    print("update did not move task to in_progress")
    sys.exit(1)
if data.get("dyad") != dyad:
    print("update did not set dyad")
    sys.exit(1)
PY
