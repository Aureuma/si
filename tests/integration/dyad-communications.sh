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

RUN_ID=$(date -u +%Y%m%d%H%M%S)
DYAD_A_BASE=${DYAD_A:-test-alpha}
DYAD_B_BASE=${DYAD_B:-test-bravo}
DYAD_A="${DYAD_A_BASE}-${RUN_ID}"
DYAD_B="${DYAD_B_BASE}-${RUN_ID}"
ROLE=${DYAD_ROLE:-qa}
DEPT=${DYAD_DEPT:-qa}

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

curl_retry() {
  local i err
  err=$(mktemp)
  for i in $(seq 1 3); do
    if curl -fsSL "$@" 2>"$err"; then
      rm -f "$err"
      return 0
    fi
    sleep 1
  done
  cat "$err" >&2
  rm -f "$err"
  return 1
}

capture_retry() {
  local i output err
  err=$(mktemp)
  for i in $(seq 1 3); do
    if output=$("$@" 2>"$err"); then
      rm -f "$err"
      printf '%s' "$output"
      return 0
    fi
    sleep 1
  done
  cat "$err" >&2
  rm -f "$err"
  return 1
}

wait_for_manager

run_retry() {
  local i err
  err=$(mktemp)
  for i in $(seq 1 3); do
    if "$@" 2>"$err"; then
      rm -f "$err"
      return 0
    fi
    sleep 1
  done
  cat "$err" >&2
  rm -f "$err"
  return 1
}

run_retry env MANAGER_URL="$MANAGER_URL" "${ROOT_DIR}/bin/register-dyad.sh" "$DYAD_A" "$ROLE" "$DEPT" >/dev/null
run_retry env MANAGER_URL="$MANAGER_URL" "${ROOT_DIR}/bin/register-dyad.sh" "$DYAD_B" "$ROLE" "$DEPT" >/dev/null

post_heartbeat() {
  local dyad="$1"
  local actor="actor-${dyad}"
  local critic="critic-${dyad}"
  local payload
  payload=$(python3 - <<'PY' "$dyad" "$ROLE" "$DEPT" "$actor" "$critic" "$RUN_ID"
import json, sys

dyad, role, dept, actor, critic, run_id = sys.argv[1:]
print(json.dumps({
    "dyad": dyad,
    "role": role,
    "department": dept,
    "actor": actor,
    "critic": critic,
    "status": "active",
    "message": f"test-run:{run_id}",
}))
PY
  )
  curl_retry -X POST -H "Content-Type: application/json" \
    -d "$payload" "${MANAGER_URL}/heartbeat" >/dev/null
}

post_heartbeat "$DYAD_A"
post_heartbeat "$DYAD_B"

DYADS_FILE=$(mktemp)
TASK_FILE=$(mktemp)
CLAIM_FILE=$(mktemp)
UPDATE_FILE=$(mktemp)
FEEDBACK_FILE=$(mktemp)
cleanup() {
  rm -f "$DYADS_FILE" "$TASK_FILE" "$CLAIM_FILE" "$UPDATE_FILE" "$FEEDBACK_FILE"
}
trap cleanup EXIT

curl_retry "${MANAGER_URL}/dyads" > "$DYADS_FILE"
python3 - <<'PY' "$DYADS_FILE" "$DYAD_A" "$DYAD_B"
import json, sys

path, a_name, b_name = sys.argv[1:4]
with open(path, "r", encoding="utf-8") as fh:
    data = json.load(fh)

def find(name):
    for item in data:
        if item.get("dyad") == name:
            return item
    return None

for name in (a_name, b_name):
    item = find(name)
    if not item:
        print(f"dyad not registered: {name}")
        sys.exit(1)
    last = item.get("last_heartbeat", "")
    if not last or last.startswith("0001-"):
        print(f"missing heartbeat for {name}")
        sys.exit(1)
PY

TASK_TITLE="Cross-dyad sync ${RUN_ID}"
TASK_DESC="Test task from ${DYAD_B} to ${DYAD_A}"
TASK_NOTES="run-id=${RUN_ID}"
TASK_JSON=$(capture_retry env MANAGER_URL="$MANAGER_URL" DYAD_TASK_KIND="test.dyad-comm" REQUESTED_BY="$DYAD_B" \
  "${ROOT_DIR}/bin/add-dyad-task.sh" "$TASK_TITLE" "$DYAD_A" "actor-${DYAD_A}" "critic-${DYAD_A}" "low" "$TASK_DESC" "" "$TASK_NOTES")
printf '%s' "$TASK_JSON" > "$TASK_FILE"

TASK_ID=$(python3 - <<'PY' "$TASK_FILE" "$DYAD_B"
import json, sys

path, requested_by = sys.argv[1:3]
with open(path, "r", encoding="utf-8") as fh:
    data = json.load(fh)

if data.get("requested_by") != requested_by:
    print("requested_by mismatch")
    sys.exit(1)
if not data.get("id"):
    print("task id missing")
    sys.exit(1)

print(data["id"])
PY
)

CLAIM_PAYLOAD=$(python3 - <<'PY' "$TASK_ID" "$DYAD_A"
import json, sys

task_id, dyad = sys.argv[1:3]
print(json.dumps({
    "id": int(task_id),
    "dyad": dyad,
    "critic": f"critic-{dyad}",
}))
PY
)

curl_retry -X POST -H "Content-Type: application/json" \
  -d "$CLAIM_PAYLOAD" "${MANAGER_URL}/dyad-tasks/claim" > "$CLAIM_FILE"

python3 - <<'PY' "$CLAIM_FILE" "$DYAD_A"
import json, sys

path, dyad = sys.argv[1:3]
with open(path, "r", encoding="utf-8") as fh:
    data = json.load(fh)

if data.get("status") != "in_progress":
    print("claim did not move task to in_progress")
    sys.exit(1)
if data.get("claimed_by") != f"critic-{dyad}":
    print("claimed_by mismatch")
    sys.exit(1)
PY

UPDATE_JSON=$(capture_retry env MANAGER_URL="$MANAGER_URL" "${ROOT_DIR}/bin/update-dyad-task.sh" \
  "$TASK_ID" "done" "completed by ${DYAD_A} for ${DYAD_B}" "actor-${DYAD_A}" "critic-${DYAD_A}")
printf '%s' "$UPDATE_JSON" > "$UPDATE_FILE"

python3 - <<'PY' "$UPDATE_FILE"
import json, sys

path = sys.argv[1]
with open(path, "r", encoding="utf-8") as fh:
    data = json.load(fh)

if data.get("status") != "done":
    print("task did not transition to done")
    sys.exit(1)
PY

MANAGER_URL="$MANAGER_URL" "${ROOT_DIR}/bin/add-feedback.sh" \
  info "dyad-comm ${RUN_ID} ${DYAD_B} -> ${DYAD_A}" "dyad-${DYAD_B}" "test.dyad-comm" >/dev/null

curl_retry "${MANAGER_URL}/feedback" > "$FEEDBACK_FILE"
python3 - <<'PY' "$FEEDBACK_FILE" "$RUN_ID"
import json, sys

path, run_id = sys.argv[1:3]
with open(path, "r", encoding="utf-8") as fh:
    data = json.load(fh)

found = False
for item in data:
    if run_id in item.get("message", ""):
        found = True
        break

if not found:
    print("feedback entry not found")
    sys.exit(1)
PY

echo "dyad communication test ok (${DYAD_A} <-> ${DYAD_B})"
