#!/usr/bin/env bash
set -euo pipefail

MANAGER_URL=${MANAGER_URL:-http://localhost:9090}
CHAT_ID=${TELEGRAM_CHAT_ID:-}
NOTIFY_URL=${NOTIFY_URL:-http://localhost:8081/notify}

fetch() { curl -fsSL "$1"; }
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

TASKS_FILE="${TMP_DIR}/tasks.json"
ACCESS_FILE="${TMP_DIR}/access.json"
FEEDBACK_FILE="${TMP_DIR}/feedback.json"

fetch "$MANAGER_URL/human-tasks" > "$TASKS_FILE"
fetch "$MANAGER_URL/access-requests" > "$ACCESS_FILE"
fetch "$MANAGER_URL/feedback" > "$FEEDBACK_FILE"

summary=$(python3 - <<'PY' "$TASKS_FILE" "$ACCESS_FILE" "$FEEDBACK_FILE"
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
feedback = load(sys.argv[3])
open_tasks = [t for t in tasks if t.get("status") != "done"]
done_tasks = [t for t in tasks if t.get("status") == "done"]
pending_access = [a for a in access if a.get("status") == "pending"]
recent_feedback = feedback[-5:]

def bar(done, total, length=10):
    if total == 0:
        return "▫" * length
    filled = int(length * done / total)
    return "█" * filled + "░" * (length - filled)

total_tasks = len(tasks)
task_bar = bar(len(done_tasks), total_tasks)
lines = []
lines.append("📊 *Silexa Status*")
lines.append(f"Tasks: {len(done_tasks)}/{total_tasks} {task_bar}")
if open_tasks:
    lines.append("🟠 Open tasks (top 5):")
    for t in open_tasks[:5]:
        lines.append(f"  • #{t.get('id')} {t.get('title')} (_{t.get('requested_by')}_)")
if pending_access:
    lines.append(f"🔒 Pending access: {len(pending_access)}")
    for a in pending_access[:5]:
        lines.append(f"  • #{a.get('id')} {a.get('requester')} → {a.get('resource')} ({a.get('action')})")
if recent_feedback:
    lines.append("💬 Recent feedback:")
    for f in recent_feedback:
        lines.append(f"  • [{f.get('severity')}] {f.get('source')}: {f.get('message')}")
print("\n".join(lines))
PY
)

echo "$summary"

if [[ -n "$CHAT_ID" ]]; then
  escaped=$(printf '%s' "$summary" | sed ':a;N;$!ba;s/\\/\\\\/g; s/"/\\"/g; s/\n/\\n/g')
  payload=$(printf '{"message":"%s","chat_id":%s}' "$escaped" "$CHAT_ID")
  curl -fsSL -X POST -H "Content-Type: application/json" -d "$payload" "$NOTIFY_URL" >/dev/null || true
fi
