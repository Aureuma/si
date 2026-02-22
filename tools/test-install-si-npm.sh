#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

version="$(sed -n 's/^const siVersion = "\(.*\)"$/\1/p' tools/si/version.go)"
[[ -n "${version}" ]] || {
  echo "failed to resolve version" >&2
  exit 1
}

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

assets_dir="${tmp_dir}/assets"
npm_out="${tmp_dir}/npm"
prefix_dir="${tmp_dir}/prefix"

mkdir -p "${assets_dir}" "${npm_out}" "${prefix_dir}"

tools/release/build-cli-release-assets.sh --version "${version}" --out-dir "${assets_dir}"
tools/release/npm/build-npm-package.sh --version "${version}" --out-dir "${npm_out}"

pack_file="$(find "${npm_out}" -maxdepth 1 -type f -name 'aureuma-si-*.tgz' | head -n1)"
[[ -n "${pack_file}" ]] || {
  echo "npm package tarball not found" >&2
  exit 1
}

npm install --silent --global --prefix "${prefix_dir}" "${pack_file}"

launcher="${prefix_dir}/bin/si"
[[ -x "${launcher}" ]] || {
  echo "si launcher not installed" >&2
  exit 1
}

SI_NPM_LOCAL_ARCHIVE_DIR="${assets_dir}" "${launcher}" version

echo "npm install smoke passed"
