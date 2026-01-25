#!/usr/bin/env bash
set -euo pipefail

go test ./agents/critic/... ./agents/shared/... ./tools/codex-init/... ./tools/codex-stdout-parser/... ./tools/silexa/...
