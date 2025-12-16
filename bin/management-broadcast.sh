#!/usr/bin/env bash
set -euo pipefail

# Broadcast a management note to manager feedback and optionally Telegram.
# Usage: management-broadcast.sh "<message>" [severity]
# Env: MANAGER_URL (default http://localhost:9090), TELEGRAM_NOTIFY_URL, TELEGRAM_CHAT_ID

if [[ $# -lt 1 ]]; then
  echo "usage: management-broadcast.sh \"<message>\" [severity]" >&2
  exit 1
fi

MESSAGE="$1"
SEVERITY="${2:-info}"
MANAGER_URL="${MANAGER_URL:-http://localhost:9090}"

payload=$(cat <<EOF
{
  "source": "management",
  "severity": "${SEVERITY}",
  "message": "${MESSAGE//\"/\\\"}",
  "context": "management-bridge"
}
EOF
)

curl -fsSL -X POST -H "Content-Type: application/json" \
  -d "${payload}" \
  "${MANAGER_URL}/feedback" >/dev/null

if [[ -n "${TELEGRAM_NOTIFY_URL:-}" ]]; then
  tpayload=$(printf '{"message":"%s","chat_id":%s}' "${MESSAGE//\"/\\\"}" "${TELEGRAM_CHAT_ID:-null}")
  curl -fsSL -X POST -H "Content-Type: application/json" \
    -d "${tpayload}" \
    "${TELEGRAM_NOTIFY_URL}" >/dev/null || true
fi

echo "Broadcast sent: ${MESSAGE} (${SEVERITY})"
