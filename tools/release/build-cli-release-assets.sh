#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Build all supported si CLI release archives and checksums.

Usage:
  tools/release/build-cli-release-assets.sh \
    [--version <vX.Y.Z>] \
    [--out-dir <path>] \
    [--repo-root <path>]

Defaults:
  --version   Parsed from tools/si/version.go
  --out-dir   <repo-root>/dist
  --repo-root Auto-detected from script location
EOF
}

die() {
  echo "error: $*" >&2
  exit 1
}

checksum_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1"
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1"
    return 0
  fi
  die "missing checksum command: need sha256sum or shasum"
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root_default="$(cd "${script_dir}/../.." && pwd)"

version=""
out_dir=""
repo_root="${repo_root_default}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      version="${2:-}"
      shift 2
      ;;
    --out-dir)
      out_dir="${2:-}"
      shift 2
      ;;
    --repo-root)
      repo_root="${2:-}"
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
[[ -f "tools/si/version.go" ]] || die "tools/si/version.go not found (bad --repo-root?)"

if [[ -z "${version}" ]]; then
  version="$(sed -n 's/^const siVersion = "\(.*\)"$/\1/p' tools/si/version.go)"
fi
[[ -n "${version}" ]] || die "failed to resolve version from tools/si/version.go"
if [[ "${version}" != v* ]]; then
  die "resolved version must include v prefix, got: ${version}"
fi

if [[ -z "${out_dir}" ]]; then
  out_dir="${repo_root}/dist"
fi
mkdir -p "${out_dir}"

targets=(
  "linux amd64"
  "linux arm64"
  "linux arm 7"
  "darwin amd64"
  "darwin arm64"
)

for target in "${targets[@]}"; do
  set -- ${target}
  goos="$1"
  goarch="$2"
  goarm="${3:-}"
  args=(
    --version "${version}"
    --goos "${goos}"
    --goarch "${goarch}"
    --out-dir "${out_dir}"
    --repo-root "${repo_root}"
  )
  if [[ -n "${goarm}" ]]; then
    args+=(--goarm "${goarm}")
  fi
  tools/release/build-cli-release-asset.sh "${args[@]}"
done

version_nov="${version#v}"
expected_files=(
  "si_${version_nov}_linux_amd64.tar.gz"
  "si_${version_nov}_linux_arm64.tar.gz"
  "si_${version_nov}_linux_armv7.tar.gz"
  "si_${version_nov}_darwin_amd64.tar.gz"
  "si_${version_nov}_darwin_arm64.tar.gz"
)

for f in "${expected_files[@]}"; do
  [[ -f "${out_dir}/${f}" ]] || die "missing expected release archive: ${out_dir}/${f}"
done

checksums_path="${out_dir}/checksums.txt"
(
  cd "${out_dir}"
  : > checksums.txt
  for f in "${expected_files[@]}"; do
    checksum_file "${f}" >> checksums.txt
  done
)

echo "created release archives:"
for f in "${expected_files[@]}"; do
  echo "  - ${out_dir}/${f}"
done
echo "created checksums:"
echo "  - ${checksums_path}"
