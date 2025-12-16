#!/usr/bin/env bash
set -euo pipefail

MANAGER_URL=${MANAGER_URL:-http://localhost:9090}
CHAT_ID=${TELEGRAM_CHAT_ID:-}
NOTIFY_URL=${NOTIFY_URL:-http://localhost:8081/notify}

fetch() { curl -fsSL "$1"; }
TASKS=$(fetch "$MANAGER_URL/human-tasks")
ACCESS=$(fetch "$MANAGER_URL/access-requests")

export TASKS ACCESS
summary=$(python3 - <<'PY'
import json, os

def load(text):
    try:
        return json.loads(text)
    except Exception:
        return []

tasks = load(os.environ.get("TASKS", "[]"))
access = load(os.environ.get("ACCESS", "[]"))
open_tasks = [t for t in tasks if t.get("status") != "done"]
pending_access = [a for a in access if a.get("status") == "pending"]
lines = ["🚧 Escalation"]
if open_tasks:
    lines.append("Open tasks:")
    for t in open_tasks[:10]:
        lines.append(f"- #{t.get('id')} {t.get('title')} (by {t.get('requested_by')})")
if pending_access:
    lines.append("Pending access:")
    for a in pending_access[:10]:
        lines.append(f"- #{a.get('id')} {a.get('requester')} -> {a.get('resource')} ({a.get('action')})")
print("\n".join(lines))
PY
)

echo "$summary"

if command -v /opt/silexa/bin/add-feedback.sh >/dev/null 2>&1; then
  /opt/silexa/bin/add-feedback.sh info "$summary" "escalate-blockers" "open tasks/pending access" >/dev/null || true
fi

if [[ -n "$CHAT_ID" ]]; then
  payload=$(printf '{"message":"%s","chat_id":%s}' "$(printf '%s' "$summary" | sed ':a;N;$!ba;s/\\/\\\\/g; s/"/\\"/g; s/\n/\\n/g')" "$CHAT_ID")
  curl -fsSL -X POST -H "Content-Type: application/json" -d "$payload" "$NOTIFY_URL" >/dev/null || true
fi
