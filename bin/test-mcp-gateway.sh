#!/usr/bin/env bash
set -euo pipefail

# Smoke test the MCP Gateway build: list catalogs and show default catalog.

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

(cd "$ROOT" && docker compose build mcp-gateway >/dev/null)

echo "Catalogs:"
(cd "$ROOT" && docker compose run --rm mcp-gateway catalog ls || true)

echo "Default docker-mcp catalog (first lines):"
set +e
(cd "$ROOT" && docker compose run --rm mcp-gateway catalog show docker-mcp --format yaml) | head -n 20 || true
set -e
