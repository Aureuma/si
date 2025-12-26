#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
GO_TEST_FLAGS=${GO_TEST_FLAGS:-}

if ! command -v go >/dev/null 2>&1; then
  echo "go is required to run unit tests" >&2
  exit 1
fi

if command -v rg >/dev/null 2>&1; then
  mapfile -t modules < <(rg --files -g 'go.mod' "$ROOT_DIR" | sort)
else
  mapfile -t modules < <(find "$ROOT_DIR" -name go.mod -print | sort)
fi

if [[ ${#modules[@]} -eq 0 ]]; then
  echo "no go modules found"
  exit 0
fi

failed=0
for mod in "${modules[@]}"; do
  dir=$(dirname "$mod")
  echo "go test ${dir}"
  if ! (cd "$dir" && go test ${GO_TEST_FLAGS} ./...); then
    failed=1
  fi
done

exit $failed
