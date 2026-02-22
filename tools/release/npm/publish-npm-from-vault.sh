#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Publish @aureuma/si to npm using SI vault-managed credentials.

Usage:
  tools/release/npm/publish-npm-from-vault.sh \
    [--file <vault-env-file>] \
    [--token-env <ENV_VAR>] \
    [--] [publish-npm-package args...]

Defaults:
  --token-env NPM_GAT_AUREUMA_VANGUARDA
  --file      uses SI vault default file

Examples:
  tools/release/npm/publish-npm-from-vault.sh -- --version v0.48.0
  tools/release/npm/publish-npm-from-vault.sh -- --version v0.48.0 --dry-run
USAGE
}

die() {
  echo "error: $*" >&2
  exit 1
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../../.." && pwd)"
publish_script="${repo_root}/tools/release/npm/publish-npm-package.sh"
[[ -x "${publish_script}" ]] || die "missing executable script: ${publish_script}"

vault_file=""
token_env="NPM_GAT_AUREUMA_VANGUARDA"
pass_args=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --file)
      vault_file="${2:-}"
      shift 2
      ;;
    --token-env)
      token_env="${2:-}"
      shift 2
      ;;
    --)
      shift
      pass_args=("$@")
      break
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      pass_args+=("$1")
      shift
      ;;
  esac
done

if [[ -z "${token_env}" ]]; then
  die "--token-env must not be empty"
fi

si_cmd="${repo_root}/si"
if [[ ! -x "${si_cmd}" ]]; then
  si_cmd="$(command -v si || true)"
fi
[[ -n "${si_cmd}" ]] || die "si CLI not found (expected ${repo_root}/si or si in PATH)"

vault_args=()
if [[ -n "${vault_file}" ]]; then
  vault_args+=(--file "${vault_file}")
fi

"${si_cmd}" vault check "${vault_args[@]}" >/dev/null
if ! "${si_cmd}" vault list "${vault_args[@]}" | awk '{print $1}' | grep -Fxq "${token_env}"; then
  die "vault key ${token_env} not found"
fi

"${si_cmd}" vault run "${vault_args[@]}" -- \
  "${publish_script}" \
  --repo-root "${repo_root}" \
  --token-env "${token_env}" \
  "${pass_args[@]}"
