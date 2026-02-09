#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Import dotenvx-managed .env files into si vault (without printing secret values).

Defaults:
  --src ../viva
  --section viva
  --identity-file $SI_VAULT_IDENTITY_FILE or ~/.si/vault/keys/age.key

Examples:
  tools/vault/import-dotenvx-to-si-vault.sh --src ../viva
  tools/vault/import-dotenvx-to-si-vault.sh --src ../viva --section viva-dev
  tools/vault/import-dotenvx-to-si-vault.sh --src ../viva --dry-run

Notes:
  - Target env is inferred per file:
      *.prod*|*.production* -> prod
      otherwise            -> dev
  - Requires: node (for npx), python3, and the si binary on PATH.
EOF
}

src="../viva"
section="viva"
dry_run="0"
identity_file="${SI_VAULT_IDENTITY_FILE:-$HOME/.si/vault/keys/age.key}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --src)
      src="$2"
      shift 2
      ;;
    --section)
      section="$2"
      shift 2
      ;;
    --identity-file)
      identity_file="$2"
      shift 2
      ;;
    --dry-run)
      dry_run="1"
      shift 1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown arg: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ ! -d "$src" ]]; then
  echo "source directory not found: $src" >&2
  exit 1
fi

if ! command -v si >/dev/null 2>&1; then
  echo "si not found on PATH" >&2
  exit 1
fi

if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 not found on PATH" >&2
  exit 1
fi

if [[ ! -f "$identity_file" ]]; then
  echo "vault identity file not found: $identity_file" >&2
  echo "hint: export SI_VAULT_IDENTITY_FILE=... or pass --identity-file" >&2
  exit 1
fi

dotenvx=(npx -y @dotenvx/dotenvx)

dotenvx_keys=()
if [[ -f "$src/.env.keys" ]]; then
  dotenvx_keys=(-fk "$src/.env.keys")
fi

dotenvx_vault=()
if [[ -f "$src/.env.vault" ]]; then
  dotenvx_vault=(-fv "$src/.env.vault")
fi

mapfile -t env_files < <(
  find "$src" -maxdepth 1 -type f -name '.env*' \
    ! -name '.env.keys' \
    ! -name '.env.vault' \
    -print \
    | sort
)

if [[ ${#env_files[@]} -eq 0 ]]; then
  echo "no .env* files found in: $src" >&2
  exit 1
fi

infer_target_env() {
  local base="$1"
  shopt -s nocasematch
  if [[ "$base" == *".prod"* || "$base" == *"production"* ]]; then
    echo "prod"
  else
    echo "dev"
  fi
  shopt -u nocasematch
}

for f in "${env_files[@]}"; do
  base="$(basename "$f")"
  target_env="$(infer_target_env "$base")"

  echo "import: $f -> si vault env=$target_env section=$section"

  # Use dotenvx to parse+decrypt; force file values to win over any machine env vars.
  # IMPORTANT: do not print values.
  json="$("${dotenvx[@]}" get \
    -f "$f" \
    --overload \
    --format json \
    --strict \
    "${dotenvx_keys[@]}" \
    "${dotenvx_vault[@]}")"

  py="$(cat <<'PY'
import json
import os
import subprocess
import sys

target_env = sys.argv[1]
section = sys.argv[2]
identity_file = sys.argv[3]
dry_run = sys.argv[4] == "1"

data = json.load(sys.stdin)
if not isinstance(data, dict):
  raise SystemExit("dotenvx output was not a JSON object")

env = os.environ.copy()
env["SI_VAULT_IDENTITY_FILE"] = identity_file

for key in sorted(data.keys()):
  val = data[key]
  if val is None:
    continue

  if dry_run:
    print(f"dry-run: {target_env}:{section}:{key}")
    continue

  # Put flags before the positional key to match Go's flag parsing rules.
  cmd = ["si", "vault", "set", "--stdin", "--env", target_env, "--format"]
  if section:
    cmd += ["--section", section]
  cmd += [key]

  # Send value via stdin to avoid shell history / argv leaks.
  p = subprocess.run(
    cmd,
    input=str(val).encode("utf-8"),
    env=env,
    stdout=subprocess.DEVNULL,
    stderr=subprocess.PIPE,
  )
  if p.returncode != 0:
    sys.stderr.write(p.stderr.decode("utf-8", errors="replace"))
    raise SystemExit(p.returncode)

  print(f"imported: {target_env}:{section}:{key}")
PY
)"

  python3 -c "$py" "$target_env" "$section" "$identity_file" "$dry_run" <<<"$json"
done
