#!/usr/bin/env bash
set -euo pipefail

# Monitor docker containers and alert if CPU or memory crosses thresholds.
# Usage: resource-guard.sh [--once]
# Env: CPU_THRESHOLD (default 80), MEM_THRESHOLD (default 80), TELEGRAM_NOTIFY_URL, TELEGRAM_CHAT_ID

CPU_THRESHOLD=${CPU_THRESHOLD:-80}
MEM_THRESHOLD=${MEM_THRESHOLD:-80}
ONCE=false
if [[ "${1:-}" == "--once" ]]; then
  ONCE=true
fi

alert() {
  local msg="$1"
  echo "$msg"
  if [[ -n "${TELEGRAM_NOTIFY_URL:-}" ]]; then
    payload=$(printf '{"message":"%s","chat_id":%s}' "${msg//\"/\\\"}" "${TELEGRAM_CHAT_ID:-null}")
    curl -fsSL -X POST -H "Content-Type: application/json" -d "$payload" "$TELEGRAM_NOTIFY_URL" >/dev/null || true
  fi
}

check_once() {
  local over=()
  while IFS= read -r line; do
    # name, cpu%, mem%, mem_usage
    name=$(echo "$line" | awk '{print $1}')
    cpu=$(echo "$line" | awk '{print $2}' | tr -d '%')
    memp=$(echo "$line" | awk '{print $3}' | tr -d '%')
    if [[ "${cpu%.*}" -ge "$CPU_THRESHOLD" || "${memp%.*}" -ge "$MEM_THRESHOLD" ]]; then
      over+=("$name cpu=${cpu}% mem=${memp}%")
    fi
  done < <(docker stats --no-stream --format "{{.Name}} {{.CPUPerc}} {{.MemPerc}}")

  if [[ ${#over[@]} -gt 0 ]]; then
    alert "Resource guard: high usage\n$(printf '%s\n' "${over[@]}")"
    return 1
  fi
  return 0
}

if $ONCE; then
  check_once
  exit $?
fi

while true; do
  check_once || true
  sleep 60
done
