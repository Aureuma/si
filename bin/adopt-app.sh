#!/usr/bin/env bash
set -euo pipefail

# Adopt an existing app into the standard Silexa app layout.
# Usage: adopt-app.sh <app-name> [options]

if [[ $# -lt 1 ]]; then
  echo "usage: adopt-app.sh <app-name> [--with-db] [--web-path <path>] [--backend-path <path>] [--infra-path <path>] [--content-path <path>] [--kind <kind>] [--status <status>] [--web-stack <stack>] [--backend-stack <stack>] [--language <lang>] [--ui <ui>] [--runtime <runtime>] [--db <db>] [--orm <orm>]" >&2
  exit 1
fi

APP="$1"
shift

WITH_DB=false
ARGS=()
for arg in "$@"; do
  if [[ "$arg" == "--with-db" ]]; then
    WITH_DB=true
  else
    ARGS+=("$arg")
  fi
done

if [[ "$WITH_DB" == "false" ]]; then
  ARGS=(--no-db "${ARGS[@]}")
fi

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
"${ROOT_DIR}/bin/start-app-project.sh" "$APP" "${ARGS[@]}"
