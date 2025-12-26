#!/usr/bin/env bash
set -euo pipefail

# One-shot bootstrap for a fresh host to hand control to infra dyad.
# - Runs host bootstrap (k8s tooling)
# - Builds core images
# - Applies k8s manifests
# - Spawns infra dyad
# - Notifies via telegram (if TELEGRAM_CHAT_ID set)

CHAT_ID=${TELEGRAM_CHAT_ID:-}
NOTIFY_URL=${NOTIFY_URL:-http://localhost:8081/notify}

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

sudo /opt/silexa/bootstrap.sh

cd "$ROOT_DIR"
export TELEGRAM_CHAT_ID=${CHAT_ID}

# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl not found; bootstrap.sh should install it" >&2
  exit 1
fi

kube apply -k "${ROOT_DIR}/infra/k8s/silexa"

# Spawn infra dyad
"${ROOT_DIR}/bin/spawn-dyad.sh" infra infra engineering

msg="Silexa bootstrap completed. Infra dyad spawned."
echo "$msg"
if [[ -n "$CHAT_ID" ]]; then
  payload=$(printf '{"message":"%s","chat_id":%s}' "$(printf '%s' "$msg" | sed ':a;N;$!ba;s/\\/\\\\/g; s/"/\\"/g; s/\n/\\n/g')" "$CHAT_ID")
  curl -fsSL -X POST -H "Content-Type: application/json" -d "$payload" "$NOTIFY_URL" >/dev/null || true
fi
