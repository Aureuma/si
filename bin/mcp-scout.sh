#!/usr/bin/env bash
set -euo pipefail

# Quick MCP catalog explorer via the mcp-gateway service.
# Usage: mcp-scout.sh

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

echo "Catalogs available:"
(cd "$ROOT" && docker compose run --rm mcp-gateway catalog ls)

echo "Top of default docker-mcp catalog (yaml excerpt):"
set +e
(cd "$ROOT" && docker compose run --rm mcp-gateway catalog show docker-mcp --format yaml) | head -n 40
set -e

echo
echo "Recommendation: review entries above for relevance, note required credentials, and record picks in manager /feedback."
