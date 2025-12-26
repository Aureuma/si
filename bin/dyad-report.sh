#!/usr/bin/env bash
set -euo pipefail

# Generate a status report for a dyad and post to manager feedback.
# Usage: dyad-report.sh <dyad-name>
# Env: MANAGER_URL (default http://localhost:9090)

if [[ $# -lt 1 ]]; then
  echo "usage: dyad-report.sh <dyad-name>" >&2
  exit 1
fi

DYAD="$1"
MANAGER_URL=${MANAGER_URL:-http://localhost:9090}

DATA=$(python3 - <<'PY' "$DYAD" "$MANAGER_URL"
import json, sys, urllib.request

dyad = sys.argv[1]
base = sys.argv[2].rstrip("/")

def fetch(path):
    with urllib.request.urlopen(base + path) as r:
        return json.loads(r.read().decode())

beats = fetch("/beats")
tasks = fetch("/dyad-tasks")

actor_name = f"actor-{dyad}"
critic_name = f"critic-{dyad}"
last_actor = None
last_critic = None
critic_id = None
for b in beats:
    if b.get("actor") == actor_name:
        last_actor = b.get("when")
        critic_id = b.get("critic")
    if b.get("critic") == critic_name:
        last_critic = b.get("when")

dyad_tasks = [t for t in tasks if t.get("dyad") == dyad]
open_tasks = [t for t in dyad_tasks if t.get("status") != "done"]

lines = []
lines.append(f"Dyad report: {dyad}")
lines.append(f"Actor: {actor_name}, last beat: {last_actor}")
lines.append(f"Critic: {critic_name}, last beat: {last_critic}, id: {critic_id}")
if open_tasks:
    lines.append("Tasks:")
    for t in open_tasks:
        lines.append(f"- #{t['id']} {t['title']} [{t['status']}] prio={t.get('priority','')} actor={t.get('actor','')} critic={t.get('critic','')}")
else:
    lines.append("Tasks: none open")

print("\n".join(lines))
PY
)

TMP_MESSAGE=$(mktemp)
cleanup() {
  rm -f "$TMP_MESSAGE"
}
trap cleanup EXIT

printf '%s' "$DATA" >"$TMP_MESSAGE"

# Post to feedback
PAYLOAD=$(python3 - <<'PY' "$DYAD" "$TMP_MESSAGE"
import json
import sys

dyad = sys.argv[1]
with open(sys.argv[2], "r", encoding="utf-8") as fh:
    message = fh.read()
payload = {
    "source": "critic-router",
    "severity": "info",
    "message": message,
    "context": f"dyad-report:{dyad}",
}
print(json.dumps(payload))
PY
)

curl -fsSL -X POST -H "Content-Type: application/json" -d "$PAYLOAD" \
  "$MANAGER_URL/feedback" >/dev/null

printf "%s\n" "$DATA"
