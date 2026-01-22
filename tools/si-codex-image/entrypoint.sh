#!/usr/bin/env bash
set -euo pipefail

CODEX_DIR="/home/si/.codex"
GH_CONFIG_DIR="/home/si/.config/gh"

mkdir -p "$CODEX_DIR" "$GH_CONFIG_DIR" /workspace

if [[ "$(id -u)" -eq 0 ]]; then
  chown -R si:si /home/si /workspace || true
fi

if [[ -n "${SI_REPO:-}" ]]; then
  REPO_NAME="${SI_REPO##*/}"
  TARGET_DIR="/workspace/${REPO_NAME}"
  if [[ ! -d "$TARGET_DIR/.git" ]]; then
    export GIT_TERMINAL_PROMPT=0
    TOKEN="${SI_GH_PAT:-${GH_TOKEN:-${GITHUB_TOKEN:-}}}"
    if [[ -n "$TOKEN" ]]; then
      export GH_TOKEN="$TOKEN"
      export GITHUB_TOKEN="$TOKEN"
      if ! gh repo clone "$SI_REPO" "$TARGET_DIR" 2>/dev/null; then
        git clone "https://${TOKEN}@github.com/${SI_REPO}.git" "$TARGET_DIR" || true
      fi
    else
      git clone "https://github.com/${SI_REPO}.git" "$TARGET_DIR" || true
    fi
  fi
fi

if [[ "$(id -u)" -eq 0 ]]; then
  exec su -s /bin/bash si -c "$*"
fi

exec "$@"
