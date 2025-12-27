#!/usr/bin/env bash
set -euo pipefail

PORT=${PORT:-19090}
CHAT_ID=${TELEGRAM_CHAT_ID:-}
NOTIFY_URL=${NOTIFY_URL:-http://localhost:8081/notify}
SERVICE_NAME=${SERVICE_NAME:-silexa-manager}
SERVICE_PORT=${SERVICE_PORT:-9090}
HEALTH_PATH=${HEALTH_PATH:-/healthz}
ROOT_PATH=${ROOT_PATH:-}

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required" >&2
  exit 1
fi

pf_pid=""
cleanup() {
  if [[ -n "$pf_pid" ]]; then
    kill "$pf_pid" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

if ! kube get svc "$SERVICE_NAME" >/dev/null 2>&1; then
  echo "service ${SERVICE_NAME} not found in namespace $(k8s_namespace)" >&2
  exit 1
fi

kube port-forward "svc/${SERVICE_NAME}" "${PORT}:${SERVICE_PORT}" >/tmp/qa-smoke-portfwd.log 2>&1 &
pf_pid=$!
sleep 2
health=$(curl -fsSL --max-time 5 "http://localhost:${PORT}${HEALTH_PATH}" || true)
root=""
if [[ -n "$ROOT_PATH" ]]; then
  root=$(curl -fsSL --max-time 5 "http://localhost:${PORT}${ROOT_PATH}" || true)
fi

result="✅ QA smoke passed"
if [[ "$health" == "" ]]; then
  result="❌ QA smoke failed"
fi
if [[ -n "$ROOT_PATH" && "$root" == "" ]]; then
  result="❌ QA smoke failed"
fi

msg="$result\nHealth: $health"
if [[ -n "$ROOT_PATH" ]]; then
  msg="$msg\nRoot: $root"
fi
echo "$msg"

if [[ -n "$CHAT_ID" ]]; then
  payload=$(printf '{"message":"%s","chat_id":%s}' "$(printf '%s' "$msg" | sed ':a;N;$!ba;s/\\/\\\\/g; s/"/\\"/g; s/\n/\\n/g')" "$CHAT_ID")
  curl -fsSL -X POST -H "Content-Type: application/json" -d "$payload" "$NOTIFY_URL" >/dev/null || true
fi
