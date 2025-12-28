#!/usr/bin/env bash
set -euo pipefail

# Beam helper: create a `beam.codex_login` dyad task.
# Temporal Beam workflow will execute the login flow and send Telegram.
#
# Usage: beam-codex-login.sh <dyad> [callback_port] [forward_port]
# Env:
#   MANAGER_URL (default http://localhost:9090)
#   REQUESTED_BY (default beam-codex-login)
#   ACTOR (default actor)
#   CRITIC (default critic)

if [[ $# -lt 1 ]]; then
  echo "usage: beam-codex-login.sh <dyad> [callback_port] [forward_port]" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

DYAD="$1"
PORT="${2:-}"
FORWARD_PORT="${3:-}"

MANAGER_URL="${MANAGER_URL:-http://localhost:9090}"
REQUESTED_BY="${REQUESTED_BY:-beam-codex-login}"
ACTOR="${ACTOR:-actor}"
CRITIC="${CRITIC:-critic}"

NOTES=""
if [[ -n "$PORT" ]]; then
  NOTES="[beam.codex_login.port]=${PORT}"
fi
if [[ -n "$FORWARD_PORT" ]]; then
  if [[ -n "$NOTES" ]]; then
    NOTES="${NOTES}"$'\n'
  fi
  NOTES="${NOTES}[beam.codex_login.forward_port]=${FORWARD_PORT}"
fi

TITLE="Beam: Codex login for dyad ${DYAD}"
DESC="Temporal Beam workflow will authenticate Codex CLI for ${DYAD} and notify Telegram with the port-forward command + URL."

DYAD_TASK_KIND="beam.codex_login" \
REQUESTED_BY="${REQUESTED_BY}" \
MANAGER_URL="${MANAGER_URL}" \
  "${ROOT_DIR}/bin/add-dyad-task.sh" \
  "${TITLE}" \
  "${DYAD}" \
  "${ACTOR}" \
  "${CRITIC}" \
  "high" \
  "${DESC}" \
  "" \
  "${NOTES}"

echo "beam.codex_login task created for dyad ${DYAD}"
