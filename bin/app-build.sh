#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: app-build.sh <app-name>" >&2
  exit 1
fi

APP="$1"
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
APP_DIR="${ROOT_DIR}/apps/${APP}"
META_FILE="${APP_DIR}/app.json"

if [[ ! -f "$META_FILE" ]]; then
  echo "missing ${META_FILE}; run bin/start-app-project.sh or bin/adopt-app.sh" >&2
  exit 1
fi

read_paths() {
  python3 - <<'PY' "$META_FILE"
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as fh:
    data = json.load(fh)
paths = data.get("paths", {})
stack = data.get("stack", {})
print(paths.get("web", ""))
print(paths.get("backend", ""))
print(stack.get("web", ""))
PY
}

mapfile -t meta < <(read_paths)
WEB_PATH="${meta[0]}"
BACKEND_PATH="${meta[1]}"
WEB_STACK="${meta[2]}"

if [[ -n "$WEB_PATH" ]]; then
  if [[ "$WEB_PATH" == "." ]]; then
    APP_PATH="apps/${APP}"
    WEB_DIR="$APP_DIR"
  else
    APP_PATH="apps/${APP}/${WEB_PATH}"
    WEB_DIR="$APP_DIR/$WEB_PATH"
  fi

  WEB_IMAGE="silexa/app-${APP}-web:local"
  WEB_DOCKERFILE="${WEB_DIR}/Dockerfile"

  if [[ -f "$WEB_DOCKERFILE" ]]; then
    echo "Building web image (custom Dockerfile): ${WEB_IMAGE}"
    docker build -t "$WEB_IMAGE" -f "$WEB_DOCKERFILE" "$WEB_DIR"
  else
    if [[ "$WEB_STACK" != "" && "$WEB_STACK" != "sveltekit" ]]; then
      echo "warning: web stack is ${WEB_STACK}; default SvelteKit template may not apply" >&2
    fi
    echo "Building web image (template): ${WEB_IMAGE}"
    docker build -t "$WEB_IMAGE" \
      -f "${ROOT_DIR}/tools/app-templates/sveltekit.Dockerfile" \
      --build-arg APP_PATH="$APP_PATH" \
      "$ROOT_DIR"
  fi
fi

if [[ -n "$BACKEND_PATH" ]]; then
  BACKEND_DIR="$APP_DIR/$BACKEND_PATH"
  BACKEND_DOCKERFILE="$BACKEND_DIR/Dockerfile"
  BACKEND_IMAGE="silexa/app-${APP}-backend:local"

  if [[ -f "$BACKEND_DOCKERFILE" ]]; then
    echo "Building backend image: ${BACKEND_IMAGE}"
    docker build -t "$BACKEND_IMAGE" -f "$BACKEND_DOCKERFILE" "$BACKEND_DIR"
  else
    echo "warning: no Dockerfile found for backend at ${BACKEND_DOCKERFILE}; skipping" >&2
  fi
fi
