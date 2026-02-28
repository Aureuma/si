#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  cat <<'USAGE'
Usage: ./tools/test-runners/orbits-catalog.sh

Runs orbit catalog/list/info regression tests.
USAGE
  exit 0
fi

ensure_repo_root
ensure_go

run_go_test -count=1 ./tools/si -run 'TestOrbits(CatalogBuildAndValidateJSON|ListReadsEnvCatalogPaths|InfoIncludesCatalogSourceForBuiltin|ListCommandJSON|LifecycleViaCatalogJSON|UpdateCommandJSON)'
