#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  cat <<'USAGE'
Usage: ./tools/test-runners/plugins-e2e.sh

Runs the full plugin command regression suite (subprocess-based tests in tools/si).
USAGE
  exit 0
fi

ensure_repo_root
ensure_go

run_go_test -count=1 ./tools/si -run 'TestPlugins'
