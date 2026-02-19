#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Import plaintext .env files into si vault (no dotenvx).

Defaults:
  --src .
  --section default
  --identity-file $SI_VAULT_IDENTITY_FILE or ~/.si/vault/keys/age.key

Examples:
  tools/vault/import-dotenv-to-si-vault.sh --src .
  tools/vault/import-dotenv-to-si-vault.sh --src . --section app-dev
  tools/vault/import-dotenv-to-si-vault.sh --src . --dry-run

Notes:
  - This reads plaintext .env files. Use for migration/bootstrap only.
  - Target env is inferred per file:
      *.prod*|*.production* -> prod
      otherwise            -> dev
  - Requires: python3 and the si binary on PATH.
EOF
}

src="."
section="default"
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

  python3 - "$f" "$target_env" "$section" "$identity_file" "$dry_run" <<'PY'
import os
import re
import subprocess
import sys

path = sys.argv[1]
target_env = sys.argv[2]
section = sys.argv[3]
identity_file = sys.argv[4]
dry_run = sys.argv[5] == "1"

key_re = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")

def parse_dotenv_lines(text: str):
  out = {}
  for raw in text.splitlines():
    line = raw.strip()
    if not line or line.startswith("#"):
      continue
    if line.startswith("export "):
      line = line[len("export "):].lstrip()
    if "=" not in line:
      continue
    k, v = line.split("=", 1)
    k = k.strip()
    v = v.strip()
    if not key_re.match(k):
      continue
    # Basic quote handling. This is intentionally conservative (migration helper).
    if len(v) >= 2 and ((v[0] == v[-1] == '"') or (v[0] == v[-1] == "'")):
      q = v[0]
      v = v[1:-1]
      if q == '"':
        v = v.encode("utf-8").decode("unicode_escape")
    out[k] = v
  return out

with open(path, "r", encoding="utf-8", errors="replace") as f:
  data = parse_dotenv_lines(f.read())

env = os.environ.copy()
env["SI_VAULT_IDENTITY_FILE"] = identity_file

for key in sorted(data.keys()):
  val = data[key]
  if dry_run:
    print(f"dry-run: {target_env}:{section}:{key}")
    continue

  cmd = ["si", "vault", "set", "--stdin", "--env", target_env, "--format"]
  if section:
    cmd += ["--section", section]
  cmd += [key]

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
done
