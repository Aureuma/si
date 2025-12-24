#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: spawn-dyad.sh <name> [role] [department]" >&2
  exit 1
fi

NAME="$1"
ROLE="${2:-generic}"
DEPT="${3:-$ROLE}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NETWORK="silexa_default"
MANAGER_URL="${MANAGER_URL:-http://manager:9090}"
CODEX_ROOT="$ROOT_DIR/data/codex"
TELEGRAM_NOTIFY_URL="${TELEGRAM_NOTIFY_URL:-http://telegram-bot:8081/notify}"
CODEX_SHARED_ROOT="${CODEX_SHARED_ROOT:-$CODEX_ROOT/shared}"
CODEX_PER_DYAD="${CODEX_PER_DYAD:-1}"
CODEX_MODEL="${CODEX_MODEL:-gpt-5.1-codex-max}"

ACTOR_EFFORT="${CODEX_ACTOR_EFFORT:-}"
CRITIC_EFFORT="${CODEX_CRITIC_EFFORT:-}"
if [[ -z "$ACTOR_EFFORT" || -z "$CRITIC_EFFORT" ]]; then
  case "${ROLE}" in
    infra)
      ACTOR_EFFORT="${ACTOR_EFFORT:-xhigh}"
      CRITIC_EFFORT="${CRITIC_EFFORT:-high}"
      ;;
    research)
      ACTOR_EFFORT="${ACTOR_EFFORT:-xhigh}"
      CRITIC_EFFORT="${CRITIC_EFFORT:-high}"
      ;;
    program_manager|pm)
      ACTOR_EFFORT="${ACTOR_EFFORT:-low}"
      CRITIC_EFFORT="${CRITIC_EFFORT:-xhigh}"
      ;;
    webdev|web)
      ACTOR_EFFORT="${ACTOR_EFFORT:-high}"
      CRITIC_EFFORT="${CRITIC_EFFORT:-high}"
      ;;
    *)
      ACTOR_EFFORT="${ACTOR_EFFORT:-high}"
      CRITIC_EFFORT="${CRITIC_EFFORT:-medium}"
      ;;
  esac
fi

if [[ -z "${TELEGRAM_CHAT_ID:-}" && -f "$ROOT_DIR/.env" ]]; then
  TELEGRAM_CHAT_ID="$(grep -E '^TELEGRAM_CHAT_ID=' "$ROOT_DIR/.env" | head -n1 | cut -d= -f2- || true)"
fi

if [[ -z "${SSH_TARGET:-}" && -f "$ROOT_DIR/configs/ssh_target" ]]; then
  # shellcheck disable=SC1090
  source "$ROOT_DIR/configs/ssh_target"
fi

# Ensure shared network exists
if ! docker network inspect "$NETWORK" >/dev/null 2>&1; then
  docker network create "$NETWORK" >/dev/null
fi

# Start actor
if docker ps -a --format '{{.Names}}' | grep -q "^silexa-actor-${NAME}$"; then
  echo "actor silexa-actor-${NAME} already exists"
else
  if [[ "$CODEX_PER_DYAD" == "1" ]]; then
    CODEX_ACTOR_DIR="$CODEX_ROOT/$NAME/actor"
  else
    CODEX_ACTOR_DIR="$CODEX_SHARED_ROOT/actor"
  fi
  mkdir -p "$CODEX_ACTOR_DIR"
  docker run -d --name "silexa-actor-${NAME}" \
    --network "$NETWORK" \
    --restart unless-stopped \
    --workdir /workspace/silexa \
    -v "$ROOT_DIR:/workspace/silexa" \
    -v "$ROOT_DIR/apps:/workspace/apps" \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v "$CODEX_ACTOR_DIR:/root/.codex" \
    -e ROLE="$ROLE" \
    -e DEPARTMENT="$DEPT" \
    -e DYAD_NAME="$NAME" \
    -e DYAD_MEMBER="actor" \
    -e CODEX_INIT_FORCE="1" \
    -e CODEX_MODEL="$CODEX_MODEL" \
    -e CODEX_REASONING_EFFORT="$ACTOR_EFFORT" \
    --label "silexa.dyad=${NAME}" \
    --label "silexa.department=${DEPT}" \
    --label "silexa.role=${ROLE}" \
    silexa/actor:local tail -f /dev/null
fi

# Start critic
if docker ps -a --format '{{.Names}}' | grep -q "^silexa-critic-${NAME}$"; then
  echo "critic silexa-critic-${NAME} already exists"
else
  if [[ "$CODEX_PER_DYAD" == "1" ]]; then
    CODEX_CRITIC_DIR="$CODEX_ROOT/$NAME/critic"
  else
    CODEX_CRITIC_DIR="$CODEX_SHARED_ROOT/critic"
  fi
  mkdir -p "$CODEX_CRITIC_DIR"
  docker run -d --name "silexa-critic-${NAME}" \
    --network "$NETWORK" \
    --restart unless-stopped \
    --user 0:0 \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v "$CODEX_CRITIC_DIR:/root/.codex" \
    -e CRITIC_ID="silexa-critic-${NAME}" \
    -e ACTOR_CONTAINER="silexa-actor-${NAME}" \
    -e MANAGER_URL="$MANAGER_URL" \
    -e CODEX_WORKDIR="/workspace/silexa" \
    -e TELEGRAM_NOTIFY_URL="$TELEGRAM_NOTIFY_URL" \
    -e TELEGRAM_CHAT_ID="${TELEGRAM_CHAT_ID:-}" \
    -e SSH_TARGET="${SSH_TARGET:-}" \
    -e DEPARTMENT="$DEPT" \
    -e ROLE="$ROLE" \
    -e DYAD_NAME="$NAME" \
    -e DYAD_MEMBER="critic" \
    -e CODEX_INIT_FORCE="1" \
    -e CODEX_MODEL="$CODEX_MODEL" \
    -e CODEX_REASONING_EFFORT="$CRITIC_EFFORT" \
    --label "silexa.dyad=${NAME}" \
    --label "silexa.department=${DEPT}" \
    --label "silexa.role=${ROLE}" \
    silexa/critic:local
fi

echo "dyad ${NAME} ready: actor=silexa-actor-${NAME}, critic=silexa-critic-${NAME}"

# Bootstrap codex presence.
docker exec -u 0 "silexa-actor-${NAME}" bash -lc 'command -v codex >/dev/null 2>&1 || npm i -g @openai/codex >/dev/null 2>&1 || true' || true
docker exec -u 0 "silexa-actor-${NAME}" bash -lc 'test -x /workspace/silexa/bin/codex-init.sh && /workspace/silexa/bin/codex-init.sh >/proc/1/fd/1 2>/proc/1/fd/2 || true' || true
