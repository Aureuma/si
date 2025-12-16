#!/usr/bin/env bash
set -euo pipefail

# One-shot bootstrap for a fresh host to hand control to infra dyad.
# - Runs host bootstrap
# - Builds core images
# - Starts compose stack (manager, telegram-bot, brokers, coder-agent)
# - Spawns infra dyad
# - Notifies via telegram (if TELEGRAM_CHAT_ID set)

CHAT_ID=${TELEGRAM_CHAT_ID:-}
NOTIFY_URL=${NOTIFY_URL:-http://localhost:8081/notify}

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

sudo /opt/silexa/bootstrap.sh

cd "$ROOT_DIR"
HOST_UID=${HOST_UID:-$(id -u)} HOST_GID=${HOST_GID:-$(id -g)} TELEGRAM_CHAT_ID=${CHAT_ID} docker compose build
HOST_UID=${HOST_UID:-$(id -u)} HOST_GID=${HOST_GID:-$(id -g)} TELEGRAM_CHAT_ID=${CHAT_ID} docker compose up -d manager telegram-bot resource-broker infra-broker coder-agent

# Spawn infra dyad
sudo "$ROOT_DIR/bin/spawn-dyad.sh" infra infra engineering

msg="Silexa bootstrap completed. Infra dyad spawned (silexa-actor-infra/silexa-critic-infra)."
echo "$msg"
if [[ -n "$CHAT_ID" ]]; then
  payload=$(printf '{"message":"%s","chat_id":%s}' "$(printf '%s' "$msg" | sed ':a;N;$!ba;s/\\/\\\\/g; s/"/\\"/g; s/\n/\\n/g')" "$CHAT_ID")
  curl -fsSL -X POST -H "Content-Type: application/json" -d "$payload" "$NOTIFY_URL" >/dev/null || true
fi
