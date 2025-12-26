#!/usr/bin/env bash
set -euo pipefail

# Monitor Kubernetes pods and alert if CPU or memory crosses thresholds.
# Usage: resource-guard.sh [--once]
# Env: CPU_THRESHOLD_M (default 800), MEM_THRESHOLD_MIB (default 800), TELEGRAM_NOTIFY_URL, TELEGRAM_CHAT_ID

CPU_THRESHOLD_M=${CPU_THRESHOLD_M:-}
MEM_THRESHOLD_MIB=${MEM_THRESHOLD_MIB:-}
if [[ -z "$CPU_THRESHOLD_M" ]]; then
  CPU_THRESHOLD_M=800
fi
if [[ -z "$MEM_THRESHOLD_MIB" ]]; then
  MEM_THRESHOLD_MIB=800
fi
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
  if ! command -v kubectl >/dev/null 2>&1; then
    echo "kubectl is required for resource-guard" >&2
    return 1
  fi
  # shellcheck source=bin/k8s-lib.sh
  source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/bin/k8s-lib.sh"

  while IFS= read -r line; do
    name=$(echo "$line" | awk '{print $1}')
    cpu_raw=$(echo "$line" | awk '{print $2}')
    mem_raw=$(echo "$line" | awk '{print $3}')
    cpu_m=${cpu_raw%m}
    mem_mib=${mem_raw%Mi}
    mem_mib=${mem_mib%Gi}
    if [[ "$mem_raw" == *Gi ]]; then
      mem_mib=$((mem_mib * 1024))
    fi
    if [[ "$cpu_m" -ge "$CPU_THRESHOLD_M" || "$mem_mib" -ge "$MEM_THRESHOLD_MIB" ]]; then
      over+=("$name cpu=${cpu_m}m mem=${mem_mib}Mi")
    fi
  done < <(kube top pods --no-headers 2>/dev/null || true)

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
