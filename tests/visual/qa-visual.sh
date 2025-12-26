#!/usr/bin/env bash
set -euo pipefail

# Run visual regression checks for a given app using the Playwright-based visual runner.
# Usage: qa-visual.sh <app-name> [--notify]
# Expects the app to provide ui-tests/targets.json under apps/<app-name>.
#
# Env:
#   TELEGRAM_NOTIFY_URL (optional) - if set and --notify used, posts summary
#   TELEGRAM_CHAT_ID (optional)    - chat id for notifications
#   PIXEL_THRESHOLD (optional)     - override default pixel mismatch tolerance
#   VISUAL_IMAGE (default silexa/visual-runner:local)

if [[ $# -lt 1 ]]; then
  echo "usage: qa-visual.sh <app-name> [--notify]" >&2
  exit 1
fi

APP="$1"
NOTIFY="false"
if [[ "${2:-}" == "--notify" ]]; then
  NOTIFY="true"
fi

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
APP_DIR="${ROOT_DIR}/apps/${APP}"
TARGETS="${APP_DIR}/ui-tests/targets.json"
ARTIFACT_DIR="${APP_DIR}/.artifacts/visual"
IMAGE="${VISUAL_IMAGE:-silexa/visual-runner:local}"

if [[ ! -f "$TARGETS" ]]; then
  echo "Missing ${TARGETS}. Create it to describe routes/viewports." >&2
  exit 1
fi

if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  echo "Building ${IMAGE}..."
  docker build -t "$IMAGE" "${ROOT_DIR}/tools/visual-runner"
fi

mkdir -p "$ARTIFACT_DIR"

echo "Running visual tests for ${APP}..."
set +e
PIXEL_THRESHOLD=${PIXEL_THRESHOLD:-100} docker run --rm \
  -e TARGETS_FILE=/app/ui-tests/targets.json \
  -e ARTIFACT_DIR=/app/.artifacts/visual \
  -e PIXEL_THRESHOLD="$PIXEL_THRESHOLD" \
  -v "${APP_DIR}:/app" \
  "$IMAGE"
STATUS=$?
set -e

SUMMARY="Visual QA for ${APP}: "
if [[ $STATUS -eq 0 ]]; then
  SUMMARY+="✅ clean"
else
  SUMMARY+="❌ diffs detected. See ${ARTIFACT_DIR}"
fi
echo "$SUMMARY"

if [[ "$NOTIFY" == "true" && -n "${TELEGRAM_NOTIFY_URL:-}" ]]; then
  payload=$(printf '{"message":"%s","chat_id":%s}' \
    "${SUMMARY//\"/\\\"}" \
    "${TELEGRAM_CHAT_ID:-null}")
  curl -fsSL -X POST -H "Content-Type: application/json" \
    -d "$payload" \
    "$TELEGRAM_NOTIFY_URL" || true
fi

exit $STATUS
