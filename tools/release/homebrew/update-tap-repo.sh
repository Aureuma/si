#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Update SI Homebrew tap formula from release checksums.

Usage:
  tools/release/homebrew/update-tap-repo.sh \
    --version <vX.Y.Z> \
    --checksums <path> \
    --tap-dir <path> \
    [--repo <owner/repo>] \
    [--commit] \
    [--push]

Defaults:
  --repo Aureuma/si
USAGE
}

die() {
  echo "error: $*" >&2
  exit 1
}

version=""
checksums_path=""
tap_dir=""
repo="Aureuma/si"
do_commit=0
do_push=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      version="${2:-}"
      shift 2
      ;;
    --checksums)
      checksums_path="${2:-}"
      shift 2
      ;;
    --tap-dir)
      tap_dir="${2:-}"
      shift 2
      ;;
    --repo)
      repo="${2:-}"
      shift 2
      ;;
    --commit)
      do_commit=1
      shift
      ;;
    --push)
      do_push=1
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

[[ -n "${version}" ]] || die "--version is required"
[[ -n "${checksums_path}" ]] || die "--checksums is required"
[[ -n "${tap_dir}" ]] || die "--tap-dir is required"
[[ -d "${tap_dir}" ]] || die "tap dir does not exist: ${tap_dir}"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../../.." && pwd)"

output_formula="${tap_dir}/Formula/si.rb"
mkdir -p "${tap_dir}/Formula"

"${repo_root}/tools/release/homebrew/render-tap-formula.sh" \
  --version "${version}" \
  --checksums "${checksums_path}" \
  --repo "${repo}" \
  --output "${output_formula}"

if [[ "${do_commit}" -eq 1 ]]; then
  git -C "${tap_dir}" add Formula/si.rb
  if git -C "${tap_dir}" diff --cached --quiet; then
    echo "no formula changes to commit"
    exit 0
  fi

  git -C "${tap_dir}" commit -m "chore: update si formula to ${version}"

  if [[ "${do_push}" -eq 1 ]]; then
    git -C "${tap_dir}" push
  fi
fi
