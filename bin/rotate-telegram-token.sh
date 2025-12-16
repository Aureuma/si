#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: rotate-telegram-token.sh <new_token>" >&2
  exit 1
fi

token="$1"
SECRET_FILE="/opt/silexa/secrets/telegram_bot_token"

printf "%s" "$token" | sudo tee "$SECRET_FILE" >/dev/null
sudo chown root:root "$SECRET_FILE"
sudo chmod 600 "$SECRET_FILE"

echo "Token updated in $SECRET_FILE. Restarting telegram-bot..."
cd /opt/silexa && HOST_UID=${HOST_UID:-$(id -u)} HOST_GID=${HOST_GID:-$(id -g)} TELEGRAM_CHAT_ID=${TELEGRAM_CHAT_ID:-} sudo -E docker compose up -d telegram-bot
