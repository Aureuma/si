#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: rotate-telegram-token.sh <new_token>" >&2
  exit 1
fi

ROOT_DIR="/opt/silexa"
# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

token="$1"
SECRET_FILE="${ROOT_DIR}/secrets/telegram_bot_token"

printf "%s" "$token" | sudo tee "$SECRET_FILE" >/dev/null
sudo chown root:root "$SECRET_FILE"
sudo chmod 600 "$SECRET_FILE"

echo "Token updated in $SECRET_FILE. Rotating Kubernetes secret..."
kube create secret generic telegram-bot-token \
  --from-file=telegram_bot_token="$SECRET_FILE" \
  --dry-run=client -o yaml | kube apply -f - >/dev/null

if kube get deployment silexa-telegram-bot >/dev/null 2>&1; then
  kube rollout restart deployment silexa-telegram-bot >/dev/null
  echo "Telegram bot restarted with new token."
else
  echo "Telegram bot deployment missing; apply infra/k8s first." >&2
  exit 1
fi
