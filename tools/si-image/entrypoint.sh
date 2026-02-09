#!/usr/bin/env bash
set -euo pipefail

CODEX_DIR="/home/si/.codex"
GH_CONFIG_DIR="/home/si/.config/gh"

mkdir -p "$CODEX_DIR" "$GH_CONFIG_DIR" /workspace

apply_host_ids() {
  local uid gid current_uid current_gid group_name user_name
  uid="${SI_HOST_UID:-}"
  gid="${SI_HOST_GID:-}"
  if [[ -z "$uid" || -z "$gid" ]]; then
    return
  fi
  if [[ "$uid" == "0" || "$gid" == "0" ]]; then
    return
  fi
  current_uid="$(id -u si 2>/dev/null || true)"
  current_gid="$(id -g si 2>/dev/null || true)"
  if [[ -z "$current_uid" || -z "$current_gid" ]]; then
    return
  fi

  group_name="$(getent group "$gid" | cut -d: -f1 || true)"
  if [[ -n "$group_name" && "$group_name" != "si" ]]; then
    usermod -g "$gid" si || true
  else
    if [[ "$current_gid" != "$gid" ]]; then
      groupmod -g "$gid" si || true
    fi
  fi

  user_name="$(getent passwd "$uid" | cut -d: -f1 || true)"
  if [[ -n "$user_name" && "$user_name" != "si" ]]; then
    return
  fi
  if [[ "$current_uid" != "$uid" ]]; then
    usermod -u "$uid" -g "$gid" si || true
  fi
}

if [[ "$(id -u)" -eq 0 ]]; then
  apply_host_ids
  if [[ -n "${SI_HOST_UID:-}" && -n "${SI_HOST_GID:-}" ]]; then
    if [[ "$(id -u si 2>/dev/null || true)" == "${SI_HOST_UID}" && "$(id -g si 2>/dev/null || true)" == "${SI_HOST_GID}" ]]; then
      chown -R si:si /home/si /workspace 2>/dev/null || true
    else
      chown -R si:si /home/si 2>/dev/null || true
    fi
  else
    chown -R si:si /home/si /workspace 2>/dev/null || true
  fi
  # Avoid "dubious ownership" errors in bind-mounted workspaces.
  git config --global --add safe.directory /workspace >/dev/null 2>&1 || true
  su -s /bin/bash si -c 'git config --global --add safe.directory /workspace >/dev/null 2>&1 || true' || true
fi

if [[ -n "${SI_REPO:-}" ]]; then
  REPO_NAME="${SI_REPO##*/}"
  TARGET_DIR="/workspace/${REPO_NAME}"
  if [[ -e "$TARGET_DIR" && ! -d "$TARGET_DIR/.git" ]]; then
    echo "clone target exists but is not a git repo: $TARGET_DIR" >&2
    exit 1
  fi
  if [[ ! -d "$TARGET_DIR/.git" ]]; then
    export GIT_TERMINAL_PROMPT=0
    TOKEN="${SI_GH_PAT:-${GH_TOKEN:-${GITHUB_TOKEN:-}}}"
    if [[ -n "$TOKEN" ]]; then
      export GH_TOKEN="$TOKEN"
      export GITHUB_TOKEN="$TOKEN"
      if ! gh repo clone "$SI_REPO" "$TARGET_DIR" 2>/dev/null; then
        if ! git clone "https://${TOKEN}@github.com/${SI_REPO}.git" "$TARGET_DIR"; then
          echo "failed to clone repo: ${SI_REPO}" >&2
          exit 1
        fi
      fi
    else
      if ! git clone "https://github.com/${SI_REPO}.git" "$TARGET_DIR"; then
        echo "failed to clone repo: ${SI_REPO}" >&2
        exit 1
      fi
    fi
  fi
fi

if [[ "$(id -u)" -eq 0 ]]; then
  cmd=()
  for arg in "$@"; do
    cmd+=("$(printf '%q' "$arg")")
  done
  exec su -s /bin/bash si -c "${cmd[*]}"
fi

exec "$@"
