#!/usr/bin/env bash
set -euo pipefail

CHAT_ID=${TELEGRAM_CHAT_ID:-}
NOTIFY_URL=${NOTIFY_URL:-http://localhost:8081/notify}

cpu=$(uptime | awk -F'load average:' '{print $2}' | tr -d ' ')
mem=$(free -m | awk '/Mem:/ {print $3"/"$2" MB"}')
disk=$(df -h /opt | awk 'NR==2 {print $5" used of "$2}' )
services=0
pods=0
if command -v kubectl >/dev/null 2>&1; then
  # shellcheck source=bin/k8s-lib.sh
  source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/bin/k8s-lib.sh"
  services=$(kube get deploy --no-headers 2>/dev/null | wc -l | tr -d ' ')
  pods=$(kube get pods --no-headers 2>/dev/null | wc -l | tr -d ' ')
fi

msg=$(cat <<MSG
🩺 *System Health*
CPU load: $cpu
Memory: $mem
Disk (/opt): $disk
Deployments: $services
Pods: $pods
MSG
)

echo "$msg"

if [[ -n "$CHAT_ID" ]]; then
  payload=$(printf '{"message":"%s","chat_id":%s}' "$(printf '%s' "$msg" | sed ':a;N;$!ba;s/\\/\\\\/g; s/"/\\"/g; s/\n/\\n/g')" "$CHAT_ID")
  curl -fsSL -X POST -H "Content-Type: application/json" -d "$payload" "$NOTIFY_URL" >/dev/null || true
fi
