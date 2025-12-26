#!/usr/bin/env bash
set -euo pipefail

# Beam helper: run `codex login` inside an actor pod, capture the callback port + OAuth URL,
# and send a ready-to-run kubectl port-forward command to Telegram.
#
# Usage: beam-codex-login.sh <dyad> [callback_port]
# Env:
#   NOTIFY_URL (default http://localhost:8081/notify)
#   TELEGRAM_CHAT_ID (optional override)
#   REQUESTED_BY (defaults to dyad name)
#   FORWARD_PORT (optional, defaults to PORT+1)
#   WAIT_FOR_LOGIN (default 1; if 1, waits until codex is logged in)
#   WAIT_TIMEOUT (default 20m; used when WAIT_FOR_LOGIN=1)

if [[ $# -lt 1 ]]; then
  echo "usage: beam-codex-login.sh <dyad> [callback_port]" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

DYAD="$1"
PORT="${2:-}"
FORWARD_PORT="${FORWARD_PORT:-}"

NOTIFY_URL=${NOTIFY_URL:-http://localhost:8081/notify}
CHAT_ID="${TELEGRAM_CHAT_ID:-}"
REQUESTED_BY="${REQUESTED_BY:-$DYAD}"
WAIT_FOR_LOGIN="${WAIT_FOR_LOGIN:-1}"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-20m}"

POD=$("${ROOT_DIR}/bin/k8s-dyad-pod.sh" "$DYAD")

OUT="/tmp/codex_login.log"
kube exec "$POD" -c actor -- bash -lc "rm -f '$OUT' && touch '$OUT'" >/dev/null 2>&1 || true

# Start codex login; prefer fixed port when supported, fall back to auto-port.
if [[ -n "$PORT" ]]; then
  kube exec "$POD" -c actor -- bash -lc "nohup bash -lc 'codex login --port ${PORT} >\"$OUT\" 2>&1' >/dev/null 2>&1 & disown || true" >/dev/null || true
  sleep 1
  if kube exec "$POD" -c actor -- bash -lc "grep -q \"unexpected argument '--port'\" \"$OUT\" 2>/dev/null"; then
    kube exec "$POD" -c actor -- bash -lc "rm -f '$OUT' && touch '$OUT' && nohup bash -lc 'codex login >\"$OUT\" 2>&1' >/dev/null 2>&1 & disown || true" >/dev/null || true
    PORT=""
  fi
else
  kube exec "$POD" -c actor -- bash -lc "nohup bash -lc 'codex login >\"$OUT\" 2>&1' >/dev/null 2>&1 & disown || true" >/dev/null 2>&1 || true
fi

AUTH_URL=""
DETECTED_PORT=""
for _ in $(seq 1 90); do
  RAW=$(kube exec "$POD" -c actor -- bash -lc "cat '$OUT' 2>/dev/null || true" || true)
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
  echo "failed to capture auth URL from ${POD}:${OUT} within 60s" >&2
  exit 1
fi

if [[ -n "$PORT" ]]; then
  DETECTED_PORT="$PORT"
fi
if [[ -z "$DETECTED_PORT" ]]; then
  echo "failed to capture callback port (localhost:<port>) from ${POD}:${OUT}" >&2
  exit 1
fi
PORT="$DETECTED_PORT"
if [[ -z "$FORWARD_PORT" ]]; then
  FORWARD_PORT="$((PORT + 1))"
fi

kubectl_cmd="kubectl $(k8s_kubeconfig) -n $(k8s_namespace) port-forward pod/${POD} ${PORT}:${FORWARD_PORT}"

# Start a socat forwarder inside the pod (requires socat in actor image).
kube exec "$POD" -c actor -- bash -lc "nohup socat tcp-listen:${FORWARD_PORT},reuseaddr,fork tcp:127.0.0.1:${PORT} >/tmp/codex_socat.log 2>&1 & disown || true" >/dev/null 2>&1 || true

export K8S_PORT_FWD_CMD="$kubectl_cmd" AUTH_URL REQUESTED_BY
PAYLOAD_JSON=$(
  python3 - <<'PY'
import html
import json
import os

cmd = os.environ.get("K8S_PORT_FWD_CMD", "").strip()
url = os.environ.get("AUTH_URL", "").strip()
msg = (
  "🔐 <b>Codex login</b>\n\n"
  "<b>🛠 Port-forward:</b>\n<pre><code>{}</code></pre>\n\n"
  "<b>🌐 URL:</b>\n<pre><code>{}</code></pre>\n"
).format(html.escape(cmd), html.escape(url))

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

echo "sent telegram message for dyad ${DYAD} (${PORT})"
echo "${kubectl_cmd}"
echo "${AUTH_URL}"

if [[ "$WAIT_FOR_LOGIN" != "1" ]]; then
  exit 0
fi

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

for _ in $(seq 1 "$DEADLINE_SECS"); do
  STATUS=$(kube exec "$POD" -c actor -- bash -lc 'codex login status 2>/dev/null || true' | tr -d '\r' || true)
  if printf '%s' "$STATUS" | grep -q "Logged in"; then
    echo "codex login confirmed in dyad ${DYAD}"
    exit 0
  fi
  sleep 1
done

echo "timed out waiting for codex login in dyad ${DYAD} (WAIT_TIMEOUT=${WAIT_TIMEOUT})" >&2
exit 1
