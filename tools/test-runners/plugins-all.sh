#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  cat <<'USAGE'
Usage: ./tools/test-runners/plugins-all.sh [--skip-unit] [--skip-policy] [--skip-catalog] [--skip-e2e]

Runs all plugin-focused test runners in a stable order.
USAGE
  exit 0
fi

SKIP_UNIT=0
SKIP_POLICY=0
SKIP_CATALOG=0
SKIP_E2E=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-unit) SKIP_UNIT=1; shift ;;
    --skip-policy) SKIP_POLICY=1; shift ;;
    --skip-catalog) SKIP_CATALOG=1; shift ;;
    --skip-e2e) SKIP_E2E=1; shift ;;
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

run_step() {
  local label="$1"
  shift
  echo "==> ${label}" >&2
  "$@"
}

if [[ "${SKIP_UNIT}" -eq 0 ]]; then
  run_step "plugins unit" "${ROOT}/tools/test-runners/plugins-unit.sh"
fi
if [[ "${SKIP_POLICY}" -eq 0 ]]; then
  run_step "plugins policy" "${ROOT}/tools/test-runners/plugins-policy.sh"
fi
if [[ "${SKIP_CATALOG}" -eq 0 ]]; then
  run_step "plugins catalog" "${ROOT}/tools/test-runners/plugins-catalog.sh"
fi
if [[ "${SKIP_E2E}" -eq 0 ]]; then
  run_step "plugins e2e" "${ROOT}/tools/test-runners/plugins-e2e.sh"
fi

echo "==> all requested plugin runners passed" >&2
