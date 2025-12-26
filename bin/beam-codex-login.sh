#!/usr/bin/env bash
set -euo pipefail

# Beam helper: run `codex login` inside a container, capture the callback port + full OAuth URL,
# and send a ready-to-run SSH tunnel command to Telegram.
#
# Usage: beam-codex-login.sh <container-or-service> [callback_port] [ssh_target]
# Env:
#   NOTIFY_URL (default http://localhost:8081/notify)
#   TELEGRAM_CHAT_ID (optional override, default from .env)
#   SSH_TARGET (ssh destination; overrides arg3; best is "<user>@<public_ip>")
#   REQUESTED_BY (defaults to container name)
#   FORWARD_PORT (optional, defaults to callback_port+1)
#   WAIT_FOR_LOGIN (default 1; if 1, waits until codex is logged in)
#   WAIT_TIMEOUT (default 20m; used when WAIT_FOR_LOGIN=1)
#
# The human completes OAuth on their machine by:
# 1) Running the tunnel command.
# 2) Opening the OAuth URL in a browser.
#
# Note: `codex login` binds to 127.0.0.1 inside the container; we start a socat sidecar in the
# container's network namespace so the SSH tunnel can reach it via the container IP.

if [[ $# -lt 1 ]]; then
  echo "usage: beam-codex-login.sh <container-or-service> [callback_port] [ssh_target]" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONTAINER="$1"
PORT="${2:-}" # optional; some codex versions don't support --port
FORWARD_PORT="${FORWARD_PORT:-}" # optional; defaults to PORT+1 when PORT is known

NOTIFY_URL=${NOTIFY_URL:-http://localhost:8081/notify}
CHAT_ID="${TELEGRAM_CHAT_ID:-}"
REQUESTED_BY="${REQUESTED_BY:-$CONTAINER}"
WAIT_FOR_LOGIN="${WAIT_FOR_LOGIN:-1}"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-20m}"

CONTAINER_ID=$("${ROOT_DIR}/bin/docker-target.sh" "$CONTAINER")
CONTAINER_IP=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$CONTAINER_ID" 2>/dev/null || true)
if [[ -z "$CONTAINER_IP" ]]; then
  echo "container $CONTAINER not found or has no IP" >&2
  exit 1
fi

if [[ -z "${SSH_TARGET:-${3:-}}" ]]; then
  # Prefer a checked-in default for reproducible "ready to run" commands.
  if [[ -f "configs/ssh_target" ]]; then
    # shellcheck disable=SC1091
    source "configs/ssh_target"
  fi
fi

USER_NAME="${SSH_USER_OVERRIDE:-$(id -un)}"
PUBLIC_IP="${PUBLIC_IP_OVERRIDE:-$(curl -4 -m 2 -fsS ifconfig.co 2>/dev/null || true)}"
if [[ -z "$PUBLIC_IP" ]]; then
  PUBLIC_IP=$(hostname -I 2>/dev/null | awk '{print $1}')
fi
DEFAULT_TARGET="${USER_NAME}@${PUBLIC_IP:-host}"
SSH_TARGET="${SSH_TARGET:-${3:-$DEFAULT_TARGET}}"

OUT="/tmp/codex_login.log"
docker exec -u 0 "$CONTAINER_ID" bash -lc "rm -f '$OUT' && touch '$OUT'" >/dev/null 2>&1 || true

# Start codex login; prefer fixed port when supported, fall back to auto-port.
if [[ -n "$PORT" ]]; then
  docker exec -u 0 "$CONTAINER_ID" bash -lc "nohup bash -lc 'codex login --port ${PORT} >\"$OUT\" 2>&1' >/dev/null 2>&1 & disown || true" >/dev/null || true
  sleep 1
  if docker exec -u 0 "$CONTAINER_ID" bash -lc "grep -q \"unexpected argument '--port'\" \"$OUT\" 2>/dev/null"; then
    docker exec -u 0 "$CONTAINER_ID" bash -lc "rm -f '$OUT' && touch '$OUT' && nohup bash -lc 'codex login >\"$OUT\" 2>&1' >/dev/null 2>&1 & disown || true" >/dev/null || true
    PORT=""
  fi
else
  docker exec -u 0 "$CONTAINER_ID" bash -lc "nohup bash -lc 'codex login >\"$OUT\" 2>&1' >/dev/null 2>&1 & disown || true" >/dev/null 2>&1 || true
fi

AUTH_URL=""
DETECTED_PORT=""
for _ in $(seq 1 90); do
  RAW=$(docker exec -u 0 "$CONTAINER_ID" bash -lc "cat '$OUT' 2>/dev/null || true" || true)
  AUTH_URL=$(printf '%s\n' "$RAW" | grep -Eo 'https://[^[:space:]]+' | head -n1 || true)
  if [[ -z "$DETECTED_PORT" ]]; then
    DETECTED_PORT=$(printf '%s\n' "$RAW" | grep -Eo 'localhost:[0-9]+' | head -n1 | cut -d: -f2 || true)
    if [[ -z "$DETECTED_PORT" ]]; then
      DETECTED_PORT=$(printf '%s\n' "$RAW" | grep -Eo '127\\.0\\.0\\.1:[0-9]+' | head -n1 | cut -d: -f2 || true)
    fi
  fi
  if [[ -n "$AUTH_URL" ]]; then
    break
  fi
  sleep 1
 done

if [[ -z "$AUTH_URL" ]]; then
  echo "failed to capture auth URL from ${CONTAINER}:${OUT} within 60s" >&2
  echo "debug: docker exec -u 0 ${CONTAINER_ID} bash -lc \"cat '${OUT}'\"" >&2
  exit 1
fi

if [[ -n "$PORT" ]]; then
  DETECTED_PORT="$PORT"
fi
if [[ -z "$DETECTED_PORT" ]]; then
  echo "failed to capture callback port (localhost:<port>) from ${CONTAINER}:${OUT}" >&2
  echo "debug: docker exec -u 0 ${CONTAINER_ID} bash -lc \"cat '${OUT}'\"" >&2
  exit 1
fi
PORT="$DETECTED_PORT"
if [[ -z "$FORWARD_PORT" ]]; then
  FORWARD_PORT="$((PORT + 1))"
fi

FORWARD_NAME="${CONTAINER}-codex-forward-${PORT}"
docker rm -f "$FORWARD_NAME" >/dev/null 2>&1 || true
docker run -d --name "$FORWARD_NAME" --network "container:${CONTAINER_ID}" alpine/socat \
  "tcp-listen:${FORWARD_PORT},reuseaddr,fork" "tcp:127.0.0.1:${PORT}" >/dev/null

TUNNEL_CMD="ssh -N -L 127.0.0.1:${PORT}:${CONTAINER_IP}:${FORWARD_PORT} ${SSH_TARGET}"
export TUNNEL_CMD AUTH_URL
PAYLOAD_JSON=$(
  python3 - <<'PY'
import html
import json
import os

tunnel = os.environ.get("TUNNEL_CMD", "").strip()
url = os.environ.get("AUTH_URL", "").strip()
msg = (
  "🔐 <b>Codex login</b>\n\n"
  "<b>🛠 Tunnel:</b>\n<pre><code>{}</code></pre>\n\n"
  "<b>🌐 URL:</b>\n<pre><code>{}</code></pre>\n"
).format(html.escape(tunnel), html.escape(url))

payload = {
  "message": msg,
  "parse_mode": "HTML",
  "disable_web_page_preview": True,
}
chat = os.environ.get("TELEGRAM_CHAT_ID","").strip()
if chat:
  payload["chat_id"] = int(chat)
print(json.dumps(payload))
PY
)

curl -fsSL -X POST -H "Content-Type: application/json" -d "$PAYLOAD_JSON" "$NOTIFY_URL" >/dev/null || {
  echo "notify failed (check NOTIFY_URL/Telegram bot)" >&2
  exit 1
}

echo "sent telegram message for ${CONTAINER} (${PORT})"
echo "${TUNNEL_CMD}"
echo "${AUTH_URL}"

if [[ "$WAIT_FOR_LOGIN" != "1" ]]; then
  exit 0
fi

# Wait until login completes, then clean up the forwarder.
DEADLINE_SECS="$(python3 - <<PY
import os
import re
v=os.environ.get("WAIT_TIMEOUT","20m")
m=re.match(r"^([0-9]+)([smhd])$", v.strip())
if not m:
  print(1200)
  raise SystemExit
n=int(m.group(1))
unit=m.group(2)
mult={"s":1,"m":60,"h":3600,"d":86400}[unit]
print(n*mult)
PY
)"

for i in $(seq 1 "$DEADLINE_SECS"); do
  STATUS=$(docker exec -i "$CONTAINER_ID" bash -lc 'codex login status 2>/dev/null || true' | tr -d '\r' || true)
  if printf '%s' "$STATUS" | grep -q "Logged in"; then
    echo "codex login confirmed in ${CONTAINER}"
    docker rm -f "$FORWARD_NAME" >/dev/null 2>&1 || true
    exit 0
  fi
  sleep 1
 done

echo "timed out waiting for codex login in ${CONTAINER} (WAIT_TIMEOUT=${WAIT_TIMEOUT})" >&2
exit 1
