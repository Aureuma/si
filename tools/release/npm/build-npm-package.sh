#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Build the @aureuma/si npm package tarball.

Usage:
  tools/release/npm/build-npm-package.sh \
    [--version <vX.Y.Z>] \
    [--repo-root <path>] \
    [--out-dir <path>]

Defaults:
  --version   Parsed from tools/si/version.go
  --repo-root Auto-detected from script location
  --out-dir   <repo-root>/dist/npm
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
[[ -d npm/si ]] || die "npm/si not found"

if [[ -z "${version}" ]]; then
  version="$(sed -n 's/^const siVersion = "\(.*\)"$/\1/p' tools/si/version.go)"
fi
[[ -n "${version}" ]] || die "unable to resolve version"
[[ "${version}" == v* ]] || die "version must start with v"

npm_version="${version#v}"
[[ -n "${npm_version}" ]] || die "invalid npm version"

if [[ -z "${out_dir}" ]]; then
  out_dir="${repo_root}/dist/npm"
fi

require_cmd node
require_cmd npm
require_cmd tar

mkdir -p "${out_dir}"
stage_dir="$(mktemp -d)"
trap 'rm -rf "${stage_dir}"' EXIT

cp -R npm/si/. "${stage_dir}/"
cp LICENSE "${stage_dir}/LICENSE"

node - <<'NODE' "${stage_dir}/package.json" "${npm_version}"
const fs = require('node:fs');
const path = process.argv[2];
const version = process.argv[3];
const pkg = JSON.parse(fs.readFileSync(path, 'utf8'));
pkg.version = version;
fs.writeFileSync(path, `${JSON.stringify(pkg, null, 2)}\n`);
NODE

pushd "${stage_dir}" >/dev/null
npm pack --silent
pack_file="$(ls -1 *.tgz | head -n1)"
popd >/dev/null

[[ -n "${pack_file:-}" ]] || die "npm pack did not produce a tarball"

mv "${stage_dir}/${pack_file}" "${out_dir}/${pack_file}"

echo "created npm package: ${out_dir}/${pack_file}"
