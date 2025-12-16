#!/usr/bin/env bash
set -euo pipefail

MANAGER_URL=${MANAGER_URL:-http://localhost:9090}
CHAT_ID=${TELEGRAM_CHAT_ID:-}
NOTIFY_URL=${NOTIFY_URL:-http://localhost:8081/notify}

fetch() { curl -fsSL "$1"; }
TASKS=$(fetch "$MANAGER_URL/human-tasks")
ACCESS=$(fetch "$MANAGER_URL/access-requests")
FEEDBACK=$(fetch "$MANAGER_URL/feedback")

export TASKS ACCESS FEEDBACK
summary=$(python3 - <<'PY'
import json, os

def load(text):
    try:
        return json.loads(text)
    except Exception:
        return []

tasks = load(os.environ.get("TASKS", "[]"))
access = load(os.environ.get("ACCESS", "[]"))
feedback = load(os.environ.get("FEEDBACK", "[]"))
open_tasks = [t for t in tasks if t.get("status") != "done"]
pending_access = [a for a in access if a.get("status") == "pending"]
recent_feedback = feedback[-5:]
lines = []
lines.append(f"Open tasks: {len(open_tasks)}")
for t in open_tasks[:5]:
    lines.append(f"- [task {t.get('id')}] {t.get('title')} (by {t.get('requested_by')})")
lines.append(f"Pending access requests: {len(pending_access)}")
for a in pending_access[:5]:
    lines.append(f"- [access {a.get('id')}] {a.get('requester')} -> {a.get('resource')} ({a.get('action')})")
lines.append("Recent feedback:")
for f in recent_feedback:
    lines.append(f"- [{f.get('severity')}] {f.get('source')}: {f.get('message')}")
print("\n".join(lines))
PY
)

echo "$summary"

if [[ -n "$CHAT_ID" ]]; then
  payload=$(printf '{"message":"%s","chat_id":%s}' "$(printf '%s' "$summary" | sed 's/\\/\\\\/g; s/"/\\"/g')" "$CHAT_ID")
  curl -fsSL -X POST -H "Content-Type: application/json" -d "$payload" "$NOTIFY_URL" >/dev/null || true
fi
