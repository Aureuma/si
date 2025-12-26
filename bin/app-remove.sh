#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: app-remove.sh <app-name> [--stack-name <name>]" >&2
  exit 1
fi

APP="$1"
shift

STACK_NAME=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --stack-name)
      shift
      STACK_NAME="${1:-}"
      ;;
    *)
      echo "unknown flag: $1" >&2
      exit 1
      ;;
  esac
  shift || true
done

if [[ -z "$STACK_NAME" ]]; then
  STACK_NAME="silexa-app-${APP}"
fi

docker stack rm "$STACK_NAME"

echo "App stack removed: ${STACK_NAME}"
