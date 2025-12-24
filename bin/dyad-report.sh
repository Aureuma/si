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
import json, sys, urllib.request, urllib.error, time
dyad = sys.argv[1]
base = sys.argv[2].rstrip("/")

def fetch(path):
    with urllib.request.urlopen(base + path) as r:
        return json.loads(r.read().decode())

beats = fetch("/beats")
tasks = fetch("/dyad-tasks")

last_actor = None
last_critic = None
critic_id = None
for b in beats:
    if b.get("actor") == f"silexa-actor-{dyad}":
        last_actor = b.get("when")
        critic_id = b.get("critic")
    if b.get("critic") == f"silexa-critic-{dyad}":
        last_critic = b.get("when")

dyad_tasks = [t for t in tasks if t.get("dyad") == dyad]
open_tasks = [t for t in dyad_tasks if t.get("status") != "done"]

lines = []
lines.append(f"Dyad report: {dyad}")
lines.append(f"Actor: silexa-actor-{dyad}, last beat: {last_actor}")
lines.append(f"Critic: silexa-critic-{dyad}, last beat: {last_critic}, id: {critic_id}")
if open_tasks:
    lines.append("Tasks:")
    for t in open_tasks:
        lines.append(f"- #{t['id']} {t['title']} [{t['status']}] prio={t.get('priority','')} actor={t.get('actor','')} critic={t.get('critic','')}")
else:
    lines.append("Tasks: none open")

print("\n".join(lines))
PY
)

# Post to feedback
PAYLOAD=$(printf '{"source":"critic-router","severity":"info","message":"%s","context":"dyad-report:%s"}' \
  "$(printf '%s' "$DATA" | sed 's/"/\\"/g')" \
  "$DYAD")

curl -fsSL -X POST -H "Content-Type: application/json" -d "$PAYLOAD" \
  "$MANAGER_URL/feedback" >/dev/null

printf "%s\n" "$DATA"
