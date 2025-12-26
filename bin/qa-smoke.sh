#!/usr/bin/env bash
set -euo pipefail

APP_IMAGE=${APP_IMAGE:-silexa/sample-go-service:local}
PORT=${PORT:-18080}
CHAT_ID=${TELEGRAM_CHAT_ID:-}
NOTIFY_URL=${NOTIFY_URL:-http://localhost:8081/notify}

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=bin/swarm-lib.sh
source "${ROOT_DIR}/bin/swarm-lib.sh"

NETWORK=${NETWORK:-$(swarm_network_name)}

name=qa-smoke-$(date +%s)

cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; }
trap cleanup EXIT

docker run -d --name "$name" --network "$NETWORK" -p ${PORT}:8080 "$APP_IMAGE" >/dev/null
sleep 2
health=$(curl -fsSL --max-time 5 http://localhost:${PORT}/healthz || true)
root=$(curl -fsSL --max-time 5 http://localhost:${PORT}/ || true)

result="✅ QA smoke passed"
if [[ "$health" != "ok" ]] || [[ "$root" == "" ]]; then
  result="❌ QA smoke failed"
fi

msg="$result\nHealth: $health\nRoot: $root"
echo "$msg"

if [[ -n "$CHAT_ID" ]]; then
  payload=$(printf '{"message":"%s","chat_id":%s}' "$(printf '%s' "$msg" | sed ':a;N;$!ba;s/\\/\\\\/g; s/"/\\"/g; s/\n/\\n/g')" "$CHAT_ID")
  curl -fsSL -X POST -H "Content-Type: application/json" -d "$payload" "$NOTIFY_URL" >/dev/null || true
fi
