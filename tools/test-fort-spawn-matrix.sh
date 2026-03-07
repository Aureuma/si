#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "[fort-spawn-matrix] delegating to Go integration test"
go test -tags=integration ./tools/si -run TestFortSpawnMatrix -count=1
