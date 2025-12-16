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
TASKS=$(fetch "$MANAGER_URL/human-tasks")
ACCESS=$(fetch "$MANAGER_URL/access-requests")
RES=$(fetch "$RES_BROKER_URL/requests")
INFRA=$(fetch "$INFRA_BROKER_URL/infra")

export TASKS ACCESS RES INFRA
msg=$(python3 - <<'PY'
import json, os

def load(x):
    try: return json.loads(x)
    except Exception: return []

tasks = load(os.environ.get("TASKS","[]"))
access = load(os.environ.get("ACCESS","[]"))
res = load(os.environ.get("RES","[]"))
infra = load(os.environ.get("INFRA","[]"))
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
