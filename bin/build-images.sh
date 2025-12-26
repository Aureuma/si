#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

images=(
  "silexa/telegram-bot:local|agents/telegram-bot"
  "silexa/resource-broker:local|agents/resource-broker"
  "silexa/infra-broker:local|agents/infra-broker"
  "silexa/manager:local|agents/manager"
  "silexa/codex-monitor:local|agents/codex-monitor"
  "silexa/router:local|agents/router"
  "silexa/actor:local|agents/actor"
  "silexa/critic:local|agents/critic"
  "silexa/coder-agent:local|agents/coder"
  "silexa/mcp-gateway:local|tools/mcp-gateway"
  "silexa/dashboard:local|agents/dashboard"
)

for entry in "${images[@]}"; do
  IFS='|' read -r tag path <<<"$entry"
  echo "Building $tag from $path"
  "$ROOT_DIR/bin/image-build.sh" -t "$tag" "$ROOT_DIR/$path"
done
