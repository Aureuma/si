#!/usr/bin/env bash
set -euo pipefail

SOURCE_DIR="${SI_INSTALL_SOURCE_DIR:-/workspace/si}"
cd "${SOURCE_DIR}"
exec go run ./tools/si/cmd/install-smoke-root "$@"
