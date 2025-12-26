#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<USAGE
usage: dyadctl.sh <command> [args]
commands:
  register <name> [role] [department] Register dyad in manager registry
  create <name> [role] [department]   Create dyad via spawn-dyad.sh
  destroy <name> [reason]             Teardown dyad via teardown-dyad.sh
  list                                List running dyads
  status <name>                       Show actor/critic service tasks for dyad
USAGE
}

if [[ $# -lt 1 ]]; then usage; exit 1; fi

CMD="$1"; shift || true
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MANAGER_URL=${MANAGER_URL:-http://localhost:9090}
# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

post_feedback() {
  local severity="$1" message="$2" source="$3" context="$4"
  if command -v "${ROOT_DIR}/bin/add-feedback.sh" >/dev/null 2>&1; then
    TELEGRAM_CHAT_ID=${TELEGRAM_CHAT_ID:-} MANAGER_URL="$MANAGER_URL" "${ROOT_DIR}/bin/add-feedback.sh" "$severity" "$message" "$source" "$context" >/dev/null || true
  fi
}

case "$CMD" in
  create)
    NAME="${1:-}"; ROLE="${2:-generic}"; DEPT="${3:-$ROLE}"
    if [[ -z "$NAME" ]]; then usage; exit 1; fi
    "${ROOT_DIR}/bin/spawn-dyad.sh" "$NAME" "$ROLE" "$DEPT"
    post_feedback info "Dyad created: $NAME (role=$ROLE, dept=$DEPT)" "dyadctl" "spawn"
    ;;
  register)
    NAME="${1:-}"; ROLE="${2:-generic}"; DEPT="${3:-$ROLE}"
    if [[ -z "$NAME" ]]; then usage; exit 1; fi
    MANAGER_URL="$MANAGER_URL" "${ROOT_DIR}/bin/register-dyad.sh" "$NAME" "$ROLE" "$DEPT"
    ;;
  destroy)
    NAME="${1:-}"; REASON="${2:-}"
    if [[ -z "$NAME" ]]; then usage; exit 1; fi
    "${ROOT_DIR}/bin/teardown-dyad.sh" "$NAME"
    post_feedback warn "Dyad destroyed: $NAME" "dyadctl" "$REASON"
    ;;
  list)
    "${ROOT_DIR}/bin/list-dyads.sh"
    ;;
  status)
    NAME="${1:-}"; if [[ -z "$NAME" ]]; then usage; exit 1; fi
    kube get pods -l "silexa.dyad=${NAME}" -o wide || true
    ;;
  *) usage; exit 1;;
esac
