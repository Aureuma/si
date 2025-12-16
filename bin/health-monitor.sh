#!/usr/bin/env bash
set -euo pipefail

CHAT_ID=${TELEGRAM_CHAT_ID:-}
NOTIFY_URL=${NOTIFY_URL:-http://localhost:8081/notify}
CPU_THRESHOLD=${CPU_THRESHOLD:-5.0}
DISK_THRESHOLD=${DISK_THRESHOLD:-90}
MEM_THRESHOLD_MB=${MEM_THRESHOLD_MB:-0}

send() {
  local msg="$1"
  echo "$msg"
  if [[ -n "$CHAT_ID" ]]; then
    payload=$(printf '{"message":"%s","chat_id":%s}' "$(printf '%s' "$msg" | sed ':a;N;$!ba;s/\\/\\\\/g; s/"/\\"/g; s/\n/\\n/g')" "$CHAT_ID")
    curl -fsSL -X POST -H "Content-Type: application/json" -d "$payload" "$NOTIFY_URL" >/dev/null || true
  fi
}

# CPU
load1=$(awk '{print $1}' /proc/loadavg)
alert=()

if (( $(echo "$load1 > $CPU_THRESHOLD" | bc -l) )); then
  alert+=("⚠️ CPU load high: $load1 > $CPU_THRESHOLD")
fi

# Disk
used_pct=$(df -P /opt | awk 'NR==2 {gsub(/%/,"",$5); print $5}')
if [[ "$used_pct" -gt "$DISK_THRESHOLD" ]]; then
  alert+=("⚠️ Disk high: ${used_pct}% > ${DISK_THRESHOLD}% (/opt)")
fi

# Memory (optional threshold)
if [[ "$MEM_THRESHOLD_MB" -gt 0 ]]; then
  used_mb=$(free -m | awk '/Mem:/ {print $3}')
  if [[ "$used_mb" -gt "$MEM_THRESHOLD_MB" ]]; then
    alert+=("⚠️ Memory high: ${used_mb}MB > ${MEM_THRESHOLD_MB}MB")
  fi
fi

if [[ ${#alert[@]} -gt 0 ]]; then
  send "🚨 System Alert\n$(printf '%s\n' "${alert[@]}")"
else
  echo "System within thresholds"
fi
