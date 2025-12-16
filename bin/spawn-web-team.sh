#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

spawn() {
  local name="$1" role="$2" dept="$3"
  sudo "$ROOT_DIR/bin/spawn-dyad.sh" "$name" "$role" "$dept"
}

spawn web-planner planner planning
spawn web-builder builder engineering
spawn web-qa qa qa

sudo "$ROOT_DIR/bin/dyadctl.sh" list
