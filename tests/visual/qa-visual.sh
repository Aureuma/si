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
ARTIFACT_ROOT="${APP_DIR}/.artifacts"
ARTIFACT_DIR="${ARTIFACT_ROOT}/visual"
IMAGE="${VISUAL_IMAGE:-silexa/visual-runner:local}"

# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

SAFE_APP=$(echo "$APP" | tr '[:upper:]' '[:lower:]' | tr -cs 'a-z0-9' '-')
POD="visual-qa-${SAFE_APP}-$(date +%s)"

if [[ ! -f "$TARGETS" ]]; then
  echo "Missing ${TARGETS}. Create it to describe routes/viewports." >&2
  exit 1
fi

mkdir -p "$ARTIFACT_DIR"

cleanup() {
  kube delete pod "$POD" --ignore-not-found >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "Starting visual runner pod ${POD}..."
kube run "$POD" --image="$IMAGE" --restart=Never --command -- sleep 3600 >/dev/null
kube wait --for=condition=Ready "pod/${POD}" --timeout=120s >/dev/null
kube exec "$POD" -- mkdir -p /app/ui-tests /app/.artifacts/visual >/dev/null
kube cp "$TARGETS" "$POD:/app/ui-tests/targets.json" >/dev/null
if [[ -d "$ARTIFACT_DIR" ]]; then
  kube cp "$ARTIFACT_DIR" "$POD:/app/.artifacts" >/dev/null
fi

echo "Running visual tests for ${APP}..."
set +e
PIXEL_THRESHOLD=${PIXEL_THRESHOLD:-100}
kube exec "$POD" -- env \
  TARGETS_FILE=/app/ui-tests/targets.json \
  ARTIFACT_DIR=/app/.artifacts/visual \
  PIXEL_THRESHOLD="$PIXEL_THRESHOLD" \
  npm test
STATUS=$?
set -e

kube cp "$POD:/app/.artifacts/visual" "$ARTIFACT_ROOT" >/dev/null

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
