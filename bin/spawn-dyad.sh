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
MANAGER_URL="${MANAGER_URL:-http://manager:9090}"
CODEX_ROOT="$ROOT_DIR/data/codex"
TELEGRAM_NOTIFY_URL="${TELEGRAM_NOTIFY_URL:-http://telegram-bot:8081/notify}"
CODEX_SHARED_ROOT="${CODEX_SHARED_ROOT:-$CODEX_ROOT/shared}"
CODEX_PER_DYAD="${CODEX_PER_DYAD:-1}"
CODEX_MODEL="${CODEX_MODEL:-gpt-5.1-codex-max}"

# shellcheck source=bin/swarm-lib.sh
source "${ROOT_DIR}/bin/swarm-lib.sh"

STACK="$(swarm_stack_name)"
NETWORK="$(swarm_network_name)"
ACTOR_NAME="actor-${NAME}"
CRITIC_NAME="critic-${NAME}"
ACTOR_SERVICE="${STACK}_${ACTOR_NAME}"
CRITIC_SERVICE="${STACK}_${CRITIC_NAME}"

if ! [[ "$NAME" =~ ^[A-Za-z0-9_-]+$ ]]; then
  echo "invalid dyad name: $NAME (allowed: letters, numbers, _ and -)" >&2
  exit 1
fi

require_registered_dyad() {
  if [[ -z "$MANAGER_URL" ]]; then
    echo "MANAGER_URL is required to verify dyad registration" >&2
    exit 1
  fi
  if ! command -v curl >/dev/null 2>&1; then
    echo "curl is required to verify dyad registration" >&2
    exit 1
  fi
  local list
  if ! list="$(curl -fsS "${MANAGER_URL%/}/dyads")"; then
    echo "unable to reach manager at ${MANAGER_URL}; set MANAGER_URL to a reachable URL" >&2
    exit 1
  fi
  if ! echo "$list" | grep -q "\"dyad\"[[:space:]]*:[[:space:]]*\"$NAME\""; then
    echo "dyad '$NAME' is not registered; run bin/register-dyad.sh $NAME $ROLE $DEPT" >&2
    exit 1
  fi
}

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

require_registered_dyad

if ! docker network inspect "$NETWORK" >/dev/null 2>&1; then
  echo "missing swarm network: $NETWORK (run bin/swarm-init.sh && bin/swarm-deploy.sh)" >&2
  exit 1
fi

# Start actor service
if docker service inspect "$ACTOR_SERVICE" >/dev/null 2>&1; then
  echo "actor service ${ACTOR_SERVICE} already exists"
  docker service update --replicas 1 "$ACTOR_SERVICE" >/dev/null 2>&1 || true
else
  if [[ "$CODEX_PER_DYAD" == "1" ]]; then
    CODEX_ACTOR_DIR="$CODEX_ROOT/$NAME/actor"
  else
    CODEX_ACTOR_DIR="$CODEX_SHARED_ROOT/actor"
  fi
  mkdir -p "$CODEX_ACTOR_DIR"
  docker service create \
    --name "$ACTOR_SERVICE" \
    --network "$NETWORK" \
    --constraint node.labels.silexa.storage==local \
    --limit-cpu "1.0" \
    --limit-memory "1G" \
    --label "com.docker.stack.namespace=${STACK}" \
    --container-label "silexa.dyad=${NAME}" \
    --container-label "silexa.department=${DEPT}" \
    --container-label "silexa.role=${ROLE}" \
    --workdir /workspace/apps \
    --mount "type=bind,src=${ROOT_DIR},dst=/workspace/silexa" \
    --mount "type=bind,src=${ROOT_DIR}/apps,dst=/workspace/apps" \
    --mount "type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock" \
    --mount "type=bind,src=${CODEX_ACTOR_DIR},dst=/root/.codex" \
    --env ROLE="$ROLE" \
    --env DEPARTMENT="$DEPT" \
    --env DYAD_NAME="$NAME" \
    --env DYAD_MEMBER="actor" \
    --env CODEX_INIT_FORCE="1" \
    --env CODEX_MODEL="$CODEX_MODEL" \
    --env CODEX_REASONING_EFFORT="$ACTOR_EFFORT" \
    silexa/actor:local \
    bash -lc "npm i -g @openai/codex >/dev/null 2>&1 || true; /workspace/silexa/bin/codex-init.sh >/proc/1/fd/1 2>/proc/1/fd/2 || true; exec tail -f /dev/null" >/dev/null
fi

# Start critic service
if docker service inspect "$CRITIC_SERVICE" >/dev/null 2>&1; then
  echo "critic service ${CRITIC_SERVICE} already exists"
  docker service update --replicas 1 "$CRITIC_SERVICE" >/dev/null 2>&1 || true
else
  if [[ "$CODEX_PER_DYAD" == "1" ]]; then
    CODEX_CRITIC_DIR="$CODEX_ROOT/$NAME/critic"
  else
    CODEX_CRITIC_DIR="$CODEX_SHARED_ROOT/critic"
  fi
  mkdir -p "$CODEX_CRITIC_DIR"
  docker service create \
    --name "$CRITIC_SERVICE" \
    --network "$NETWORK" \
    --constraint node.labels.silexa.storage==local \
    --limit-cpu "0.75" \
    --limit-memory "512M" \
    --label "com.docker.stack.namespace=${STACK}" \
    --container-label "silexa.dyad=${NAME}" \
    --container-label "silexa.department=${DEPT}" \
    --container-label "silexa.role=${ROLE}" \
    --user 0:0 \
    --mount "type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock" \
    --mount "type=bind,src=${CODEX_CRITIC_DIR},dst=/root/.codex" \
    --mount "type=bind,src=${ROOT_DIR}/configs,dst=/configs,readonly" \
    --env CRITIC_ID="$CRITIC_NAME" \
    --env ACTOR_CONTAINER="$ACTOR_NAME" \
    --env MANAGER_URL="$MANAGER_URL" \
    --env CODEX_WORKDIR="/workspace/silexa" \
    --env TELEGRAM_NOTIFY_URL="$TELEGRAM_NOTIFY_URL" \
    --env TELEGRAM_CHAT_ID="${TELEGRAM_CHAT_ID:-}" \
    --env SSH_TARGET="${SSH_TARGET:-}" \
    --env DEPARTMENT="$DEPT" \
    --env ROLE="$ROLE" \
    --env DYAD_NAME="$NAME" \
    --env DYAD_MEMBER="critic" \
    --env CODEX_INIT_FORCE="1" \
    --env CODEX_MODEL="$CODEX_MODEL" \
    --env CODEX_REASONING_EFFORT="$CRITIC_EFFORT" \
    --env SILEXA_STACK="$STACK" \
    silexa/critic:local >/dev/null
fi

echo "dyad ${NAME} ready: actor=${ACTOR_NAME}, critic=${CRITIC_NAME}"

# Bootstrap codex presence.
ACTOR_ID=$("${ROOT_DIR}/bin/docker-target.sh" "$ACTOR_NAME" || true)
if [[ -n "$ACTOR_ID" ]]; then
  docker exec -u 0 "$ACTOR_ID" bash -lc 'command -v codex >/dev/null 2>&1 || npm i -g @openai/codex >/dev/null 2>&1 || true' || true
  docker exec -u 0 "$ACTOR_ID" bash -lc 'test -x /workspace/silexa/bin/codex-init.sh && /workspace/silexa/bin/codex-init.sh >/proc/1/fd/1 2>/proc/1/fd/2 || true' || true
fi
