#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Publish @aureuma/si to npm.

Usage:
  tools/release/npm/publish-npm-package.sh \
    [--version <vX.Y.Z>] \
    [--repo-root <path>] \
    [--out-dir <path>] \
    [--token-env <ENV_VAR>] \
    [--dry-run]

Defaults:
  --version   Parsed from tools/si/version.go
  --repo-root Auto-detected from script location
  --out-dir   <repo-root>/dist/npm
  --token-env NPM_TOKEN (falls back to NPM_GAT_AUREUMA_VANGUARDA when unset)
USAGE
}

die() {
  echo "error: $*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root_default="$(cd "${script_dir}/../../.." && pwd)"

version=""
repo_root="${repo_root_default}"
out_dir=""
token_env="NPM_TOKEN"
dry_run=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      version="${2:-}"
      shift 2
      ;;
    --repo-root)
      repo_root="${2:-}"
      shift 2
      ;;
    --out-dir)
      out_dir="${2:-}"
      shift 2
      ;;
    --token-env)
      token_env="${2:-}"
      shift 2
      ;;
    --dry-run)
      dry_run=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

cd "${repo_root}"
[[ -f tools/si/version.go ]] || die "tools/si/version.go not found"

if [[ "${token_env}" == "NPM_TOKEN" && -z "${NPM_TOKEN:-}" && -n "${NPM_GAT_AUREUMA_VANGUARDA:-}" ]]; then
  token_env="NPM_GAT_AUREUMA_VANGUARDA"
fi

if [[ -z "${version}" ]]; then
  version="$(sed -n 's/^const siVersion = "\(.*\)"$/\1/p' tools/si/version.go)"
fi
[[ -n "${version}" ]] || die "unable to resolve version"
[[ "${version}" == v* ]] || die "version must start with v"

npm_version="${version#v}"

if [[ -z "${out_dir}" ]]; then
  out_dir="${repo_root}/dist/npm"
fi

require_cmd npm
require_cmd node

if npm view "@aureuma/si@${npm_version}" version >/dev/null 2>&1; then
  echo "@aureuma/si@${npm_version} already published; skipping"
  exit 0
fi

"${repo_root}/tools/release/npm/build-npm-package.sh" \
  --version "${version}" \
  --repo-root "${repo_root}" \
  --out-dir "${out_dir}"

pack_file="$(find "${out_dir}" -maxdepth 1 -type f -name 'aureuma-si-*.tgz' | sort | tail -n1)"
[[ -n "${pack_file}" ]] || die "failed to find generated package tarball"

token="${!token_env:-}"
if [[ -z "${token}" ]]; then
  die "token environment variable ${token_env} is required"
fi

npmrc_path="$(mktemp)"
trap 'rm -f "${npmrc_path}"' EXIT
cat > "${npmrc_path}" <<NPMRC
//registry.npmjs.org/:_authToken=${token}
always-auth=true
NPMRC

export NPM_CONFIG_USERCONFIG="${npmrc_path}"

if [[ "${dry_run}" -eq 1 ]]; then
  npm publish "${pack_file}" --access public --dry-run
  echo "dry-run complete: ${pack_file}"
  exit 0
fi

npm publish "${pack_file}" --access public

if ! npm view "@aureuma/si@${npm_version}" version >/dev/null 2>&1; then
  die "package publish appears to have failed verification"
fi

echo "published @aureuma/si@${npm_version}"
