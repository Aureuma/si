#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: rotate-telegram-token.sh <new_token>" >&2
  exit 1
fi

ROOT_DIR="/opt/silexa"
# shellcheck source=bin/swarm-lib.sh
source "${ROOT_DIR}/bin/swarm-lib.sh"

STACK="$(swarm_stack_name)"
SERVICE="${STACK}_telegram-bot"

token="$1"
SECRET_FILE="${ROOT_DIR}/secrets/telegram_bot_token"

printf "%s" "$token" | sudo tee "$SECRET_FILE" >/dev/null
sudo chown root:root "$SECRET_FILE"
sudo chmod 600 "$SECRET_FILE"

echo "Token updated in $SECRET_FILE. Rotating swarm secret..."
if docker service inspect "$SERVICE" >/dev/null 2>&1; then
  docker service update --secret-rm telegram_bot_token "$SERVICE" >/dev/null 2>&1 || true
fi

docker secret rm telegram_bot_token >/dev/null 2>&1 || true
docker secret create telegram_bot_token "$SECRET_FILE" >/dev/null

if docker service inspect "$SERVICE" >/dev/null 2>&1; then
  docker service update --secret-add source=telegram_bot_token,target=telegram_bot_token "$SERVICE" >/dev/null
  docker service update --force "$SERVICE" >/dev/null
  echo "Telegram bot restarted with new token."
else
  echo "Telegram bot service missing (${SERVICE}); deploy stack first." >&2
  exit 1
fi
