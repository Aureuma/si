#!/usr/bin/env bash
set -euo pipefail

# Pre-deploy checklist to avoid surprise costs. Summarizes pending resource/infra requests and access approvals.
MANAGER_URL=${MANAGER_URL:-http://localhost:9090}
RES_BROKER_URL=${RES_BROKER_URL:-http://localhost:9091}
INFRA_BROKER_URL=${INFRA_BROKER_URL:-http://localhost:9092}
CHAT_ID=${TELEGRAM_CHAT_ID:-}
NOTIFY_URL=${NOTIFY_URL:-http://localhost:8081/notify}
EMOJI=${COST_EMOJI:-💸}

fetch() { curl -fsSL "$1"; }
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

TASKS_FILE="${TMP_DIR}/tasks.json"
ACCESS_FILE="${TMP_DIR}/access.json"
RES_FILE="${TMP_DIR}/resource.json"
INFRA_FILE="${TMP_DIR}/infra.json"

fetch "$MANAGER_URL/human-tasks" > "$TASKS_FILE"
fetch "$MANAGER_URL/access-requests" > "$ACCESS_FILE"
fetch "$RES_BROKER_URL/requests" > "$RES_FILE"
fetch "$INFRA_BROKER_URL/infra" > "$INFRA_FILE"

msg=$(python3 - <<'PY' "$TASKS_FILE" "$ACCESS_FILE" "$RES_FILE" "$INFRA_FILE"
import json
import sys

def load(path):
    try:
        with open(path, "r", encoding="utf-8") as f:
            return json.load(f)
    except Exception:
        return []

tasks = load(sys.argv[1])
access = load(sys.argv[2])
res = load(sys.argv[3])
infra = load(sys.argv[4])
open_tasks = [t for t in tasks if t.get("status") != "done"]
pending_access = [a for a in access if a.get("status") == "pending"]
pending_res = [r for r in res if r.get("status") == "pending"]
pending_infra = [i for i in infra if i.get("status") == "pending"]
lines = []
lines.append("Pre-deploy checklist")
lines.append(f"Open tasks: {len(open_tasks)}")
lines.append(f"Pending access: {len(pending_access)}")
lines.append(f"Pending resource reqs: {len(pending_res)}")
lines.append(f"Pending infra reqs: {len(pending_infra)}")
if pending_res:
    lines.append("Resource reqs:")
    for r in pending_res[:5]:
        lines.append(f"- #{r.get('id')} {r.get('resource')}:{r.get('action')} by {r.get('requested_by')}")
if pending_infra:
    lines.append("Infra reqs:")
    for i in pending_infra[:5]:
        lines.append(f"- #{i.get('id')} {i.get('category')}:{i.get('action')} by {i.get('requested_by')}")
print("\n".join(lines))
PY
)

echo "$msg"

if [[ -n "$CHAT_ID" ]]; then
  payload=$(printf '{"message":"%s %s","chat_id":%s}' "$EMOJI" "$(printf '%s' "$msg" | sed ':a;N;$!ba;s/\\/\\\\/g; s/"/\\"/g; s/\n/\\n/g')" "$CHAT_ID")
  curl -fsSL -X POST -H "Content-Type: application/json" -d "$payload" "$NOTIFY_URL" >/dev/null || true
fi
