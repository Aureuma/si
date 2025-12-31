#!/usr/bin/env bash
set -euo pipefail

# Initializes a baseline Codex CLI config for Silexa dyads.
# - Writes `~/.codex/config.toml` with dyad/role metadata (safe; Codex ignores unknown tables).
# - Designed to be idempotent and safe to run on every container start.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

HOME_DIR="${HOME:-/root}"
CODEX_HOME_DIR="${CODEX_HOME:-$HOME_DIR/.codex}"
CODEX_CONFIG_DIR="${CODEX_CONFIG_DIR:-$CODEX_HOME_DIR}"
CFG="${CODEX_CONFIG_DIR}/config.toml"
CODEX_CONFIG_TEMPLATE="${CODEX_CONFIG_TEMPLATE:-$ROOT_DIR/configs/codex-config.template.toml}"

DYAD_NAME="${DYAD_NAME:-unknown}"
DYAD_MEMBER="${DYAD_MEMBER:-unknown}" # actor|critic
ROLE="${ROLE:-unknown}"
DEPARTMENT="${DEPARTMENT:-unknown}"
CODEX_INIT_FORCE="${CODEX_INIT_FORCE:-0}"
CODEX_MODEL="${CODEX_MODEL:-}"
CODEX_REASONING_EFFORT="${CODEX_REASONING_EFFORT:-}"

mkdir -p "$CODEX_CONFIG_DIR"

escape_replace() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/\"/\\"/g' -e 's/[|&]/\\&/g'
}

managed=0
if [[ -f "$CFG" ]] && grep -q "managed by silexa-codex-init" "$CFG" 2>/dev/null; then
  managed=1
fi

if [[ ! -f "$CFG" || "$CODEX_INIT_FORCE" == "1" || "$managed" == "1" ]]; then
  tmp="$(mktemp)"
  now="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  model="${CODEX_MODEL:-gpt-5.2-codex}"
  effort="${CODEX_REASONING_EFFORT:-medium}"
  if [[ -f "$CODEX_CONFIG_TEMPLATE" ]]; then
    sed \
      -e "s|__CODEX_MODEL__|$(escape_replace "$model")|g" \
      -e "s|__CODEX_REASONING_EFFORT__|$(escape_replace "$effort")|g" \
      -e "s|__DYAD_NAME__|$(escape_replace "$DYAD_NAME")|g" \
      -e "s|__DYAD_MEMBER__|$(escape_replace "$DYAD_MEMBER")|g" \
      -e "s|__ROLE__|$(escape_replace "$ROLE")|g" \
      -e "s|__DEPARTMENT__|$(escape_replace "$DEPARTMENT")|g" \
      -e "s|__INITIALIZED_UTC__|$(escape_replace "$now")|g" \
      "$CODEX_CONFIG_TEMPLATE" >"$tmp"
  else
    cat >"$tmp" <<EOF
# managed by silexa-codex-init
#
# Shared Codex defaults for Silexa dyads.

# Codex defaults (set via env in dyad containers)
model = "$(printf '%s' "${CODEX_MODEL:-gpt-5.2-codex}" | sed 's/\"/\\"/g')"
model_reasoning_effort = "$(printf '%s' "${CODEX_REASONING_EFFORT:-medium}" | sed 's/\"/\\"/g')"

[features]
web_search_request = true

[sandbox_workspace_write]
network_access = true

[silexa]
dyad = "$(printf '%s' "$DYAD_NAME" | sed 's/\"/\\"/g')"
member = "$(printf '%s' "$DYAD_MEMBER" | sed 's/\"/\\"/g')"
role = "$(printf '%s' "$ROLE" | sed 's/\"/\\"/g')"
department = "$(printf '%s' "$DEPARTMENT" | sed 's/\"/\\"/g')"
initialized_utc = "$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
EOF
  fi
  chmod 600 "$tmp" || true
  mv -f "$tmp" "$CFG"
  chmod 600 "$CFG" || true
fi

echo "codex-init ok (dyad=${DYAD_NAME} member=${DYAD_MEMBER} role=${ROLE} dept=${DEPARTMENT})"
