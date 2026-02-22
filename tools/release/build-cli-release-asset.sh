#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Build one release archive for the si CLI.

Usage:
  tools/release/build-cli-release-asset.sh \
    --version <vX.Y.Z> \
    --goos <linux|darwin> \
    --goarch <amd64|arm64|arm> \
    [--goarm <6|7>] \
    [--out-dir <path>] \
    [--repo-root <path>]

Example:
  tools/release/build-cli-release-asset.sh \
    --version v0.48.0 \
    --goos linux \
    --goarch arm \
    --goarm 7 \
    --out-dir dist
EOF
}

die() {
  echo "error: $*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

is_gnu_tar() {
  tar --version 2>/dev/null | head -n 1 | grep -qi "gnu tar"
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root_default="$(cd "${script_dir}/../.." && pwd)"

version=""
goos=""
goarch=""
goarm=""
out_dir=""
repo_root="${repo_root_default}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      version="${2:-}"
      shift 2
      ;;
    --goos)
      goos="${2:-}"
      shift 2
      ;;
    --goarch)
      goarch="${2:-}"
      shift 2
      ;;
    --goarm)
      goarm="${2:-}"
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

[[ -n "${version}" ]] || die "--version is required"
[[ -n "${goos}" ]] || die "--goos is required"
[[ -n "${goarch}" ]] || die "--goarch is required"

case "${goos}" in
  linux|darwin) ;;
  *)
    die "unsupported --goos value: ${goos} (expected linux or darwin)"
    ;;
esac

case "${goarch}" in
  amd64|arm64) ;;
  arm)
    [[ -n "${goarm}" ]] || die "--goarm is required when --goarch=arm"
    ;;
  *)
    die "unsupported --goarch value: ${goarch} (expected amd64, arm64, or arm)"
    ;;
esac

if [[ -n "${goarm}" && "${goarch}" != "arm" ]]; then
  die "--goarm can only be used with --goarch=arm"
fi

if [[ "${version}" != v* ]]; then
  die "--version must include the v prefix (example: v0.48.0)"
fi

if [[ -z "${out_dir}" ]]; then
  out_dir="${repo_root}/dist"
fi

require_cmd go
require_cmd tar

cd "${repo_root}"
[[ -f "tools/si/go.mod" ]] || die "tools/si/go.mod not found (bad --repo-root?)"
[[ -f "README.md" ]] || die "README.md not found in repo root"
[[ -f "LICENSE" ]] || die "LICENSE not found in repo root"

mkdir -p "${out_dir}"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

version_nov="${version#v}"
arch_label="${goarch}"
if [[ "${goarch}" == "arm" ]]; then
  arch_label="armv${goarm}"
fi

artifact_stem="si_${version_nov}_${goos}_${arch_label}"
staging_dir="${tmp_dir}/${artifact_stem}"
mkdir -p "${staging_dir}"

echo "building ${artifact_stem}.tar.gz"
build_env=(
  "CGO_ENABLED=0"
  "GOOS=${goos}"
  "GOARCH=${goarch}"
)
if [[ "${goarch}" == "arm" ]]; then
  build_env+=("GOARM=${goarm}")
fi

env "${build_env[@]}" \
  go build -trimpath -buildvcs=false -ldflags "-s -w" -o "${staging_dir}/si" ./tools/si

chmod 0755 "${staging_dir}/si"
cp README.md "${staging_dir}/README.md"
cp LICENSE "${staging_dir}/LICENSE"

archive_path="${out_dir}/${artifact_stem}.tar.gz"
if is_gnu_tar; then
  tar \
    --sort=name \
    --owner=0 \
    --group=0 \
    --numeric-owner \
    --mtime='UTC 1970-01-01' \
    -C "${tmp_dir}" \
    -czf "${archive_path}" \
    "${artifact_stem}"
else
  tar -C "${tmp_dir}" -czf "${archive_path}" "${artifact_stem}"
fi

echo "created ${archive_path}"
