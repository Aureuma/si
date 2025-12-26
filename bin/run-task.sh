#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: run-task.sh <dyad> <command...>" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

DYAD="$1"
shift

POD=$("${ROOT_DIR}/bin/k8s-dyad-pod.sh" "$DYAD")

TTY_FLAG="-i"
if [[ -t 0 ]]; then
  TTY_FLAG="-it"
fi

kube exec $TTY_FLAG "$POD" -c actor -- "$@"
