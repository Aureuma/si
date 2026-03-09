#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

if ! command -v go >/dev/null 2>&1; then
  echo "error: go is required for tools/test-install-si.sh (missing from PATH)" >&2
  exit 127
fi

exec go run ./tools/si/cmd/test-install-si "$@"
