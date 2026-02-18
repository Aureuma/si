#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: ./tools/test-all.sh [flags]

Run SI test stack in a stable order:
  1) Go workspace tests
  2) Installer host smoke
  3) Installer docker smoke

Flags:
  --skip-go          Skip Go workspace tests
  --skip-installer   Skip installer host smoke tests
  --skip-docker      Skip installer docker smoke tests
  -h, --help         Show this help
USAGE
}

SKIP_GO=0
SKIP_INSTALLER=0
SKIP_DOCKER=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-go) SKIP_GO=1; shift ;;
    --skip-installer) SKIP_INSTALLER=1; shift ;;
    --skip-docker) SKIP_DOCKER=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [[ ! -f go.work ]]; then
  echo "go.work not found. Run this script from the repo root." >&2
  exit 1
fi

run_step() {
  local title="$1"
  shift
  echo "==> ${title}" >&2
  "$@"
}

if [[ "$SKIP_GO" -eq 0 ]]; then
  run_step "Go workspace tests" ./tools/test.sh
fi

if [[ "$SKIP_INSTALLER" -eq 0 ]]; then
  run_step "Installer host smoke" ./tools/test-install-si.sh
fi

if [[ "$SKIP_DOCKER" -eq 0 ]]; then
  run_step "Installer docker smoke" ./tools/test-install-si-docker.sh
fi

echo "==> all requested tests passed" >&2
