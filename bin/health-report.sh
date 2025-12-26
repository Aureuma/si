#!/usr/bin/env bash
set -euo pipefail

CHAT_ID=${TELEGRAM_CHAT_ID:-}
NOTIFY_URL=${NOTIFY_URL:-http://localhost:8081/notify}

cpu=$(uptime | awk -F'load average:' '{print $2}' | tr -d ' ')
mem=$(free -m | awk '/Mem:/ {print $3"/"$2" MB"}')
disk=$(df -h /opt | awk 'NR==2 {print $5" used of "$2}' )
services=$(docker service ls --format '{{.Name}}' | wc -l)
containers=$(docker ps --format '{{.Names}}' | wc -l)

msg=$(cat <<MSG
🩺 *System Health*
CPU load: $cpu
Memory: $mem
Disk (/opt): $disk
Services: $services
Running containers: $containers
MSG
)

echo "$msg"

if [[ -n "$CHAT_ID" ]]; then
  payload=$(printf '{"message":"%s","chat_id":%s}' "$(printf '%s' "$msg" | sed ':a;N;$!ba;s/\\/\\\\/g; s/"/\\"/g; s/\n/\\n/g')" "$CHAT_ID")
  curl -fsSL -X POST -H "Content-Type: application/json" -d "$payload" "$NOTIFY_URL" >/dev/null || true
fi
