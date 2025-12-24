#!/usr/bin/env bash
set -euo pipefail

# Initializes a baseline Codex CLI config for Silexa dyads.
# - Writes `~/.config/codex/config.toml` with dyad/role metadata (safe; Codex ignores unknown tables).
# - Designed to be idempotent and safe to run on every container start.

HOME_DIR="${HOME:-/root}"
CODEX_CONFIG_DIR="${CODEX_CONFIG_DIR:-$HOME_DIR/.config/codex}"
CFG="${CODEX_CONFIG_DIR}/config.toml"

DYAD_NAME="${DYAD_NAME:-unknown}"
DYAD_MEMBER="${DYAD_MEMBER:-unknown}" # actor|critic
ROLE="${ROLE:-unknown}"
DEPARTMENT="${DEPARTMENT:-unknown}"
CODEX_INIT_FORCE="${CODEX_INIT_FORCE:-0}"
CODEX_MODEL="${CODEX_MODEL:-}"
CODEX_REASONING_EFFORT="${CODEX_REASONING_EFFORT:-}"

mkdir -p "$CODEX_CONFIG_DIR"

managed=0
if [[ -f "$CFG" ]] && grep -q "managed by silexa-codex-init" "$CFG" 2>/dev/null; then
  managed=1
fi

if [[ ! -f "$CFG" || "$CODEX_INIT_FORCE" == "1" || "$managed" == "1" ]]; then
  tmp="$(mktemp)"
  cat >"$tmp" <<EOF
# managed by silexa-codex-init
#
# This file intentionally only stores Silexa metadata; it should not override
# Codex runtime behavior unless explicitly added later.

# Codex defaults (set via env in dyad containers)
model = "$(printf '%s' "${CODEX_MODEL:-gpt-5.1-codex-max}" | sed 's/\"/\\"/g')"
model_reasoning_effort = "$(printf '%s' "${CODEX_REASONING_EFFORT:-high}" | sed 's/\"/\\"/g')"

[silexa]
dyad = "$(printf '%s' "$DYAD_NAME" | sed 's/\"/\\"/g')"
member = "$(printf '%s' "$DYAD_MEMBER" | sed 's/\"/\\"/g')"
role = "$(printf '%s' "$ROLE" | sed 's/\"/\\"/g')"
department = "$(printf '%s' "$DEPARTMENT" | sed 's/\"/\\"/g')"
initialized_utc = "$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
EOF
  chmod 600 "$tmp" || true
  mv -f "$tmp" "$CFG"
  chmod 600 "$CFG" || true
fi

echo "codex-init ok (dyad=${DYAD_NAME} member=${DYAD_MEMBER} role=${ROLE} dept=${DEPARTMENT})"
