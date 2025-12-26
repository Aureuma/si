#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
SCOPE=${SCOPE:-smoke,go,integration}
VISUAL_APP=${VISUAL_APP:-}

usage() {
  cat <<USAGE
usage: tests/run.sh [--scope list] [--visual-app name] [--all]

scopes: smoke, go, integration, visual
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --scope)
      SCOPE="$2"
      shift 2
      ;;
    --visual-app)
      VISUAL_APP="$2"
      shift 2
      ;;
    --all)
      SCOPE="smoke,go,integration,visual"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown arg: $1" >&2
      usage
      exit 1
      ;;
  esac
done

IFS=',' read -r -a scopes <<< "$SCOPE"

run_script() {
  local script="$1"
  shift
  echo "==> ${script}"
  "${script}" "$@"
}

for scope in "${scopes[@]}"; do
  case "$scope" in
    smoke)
      run_script "${ROOT_DIR}/tests/smoke/stack-health.sh"
      run_script "${ROOT_DIR}/tests/smoke/mcp-gateway.sh"
      run_script "${ROOT_DIR}/tests/smoke/qa-smoke.sh"
      ;;
    go)
      run_script "${ROOT_DIR}/tests/go/run-go-tests.sh"
      ;;
    integration)
      run_script "${ROOT_DIR}/tests/integration/app-management.sh"
      run_script "${ROOT_DIR}/tests/integration/dyad-communications.sh"
      ;;
    visual)
      if [[ -z "$VISUAL_APP" ]]; then
        echo "visual scope requires --visual-app <app>" >&2
        exit 1
      fi
      run_script "${ROOT_DIR}/tests/visual/qa-visual.sh" "$VISUAL_APP"
      ;;
    *)
      echo "unknown scope: $scope" >&2
      exit 1
      ;;
  esac
done
