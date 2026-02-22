#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Render a Homebrew core-ready source formula template for SI.

Usage:
  tools/release/homebrew/render-core-formula.sh \
    --version <vX.Y.Z> \
    --output <path> \
    [--repo <owner/repo>]

Defaults:
  --repo Aureuma/si
USAGE
}

die() {
  echo "error: $*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

sha256_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${file}" | awk '{print $1}'
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${file}" | awk '{print $1}'
    return 0
  fi
  die "missing checksum command"
}

version=""
output_path=""
repo="Aureuma/si"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      version="${2:-}"
      shift 2
      ;;
    --output)
      output_path="${2:-}"
      shift 2
      ;;
    --repo)
      repo="${2:-}"
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
[[ "${version}" == v* ]] || die "--version must include v prefix"
[[ -n "${output_path}" ]] || die "--output is required"

require_cmd curl

source_url="https://github.com/${repo}/archive/refs/tags/${version}.tar.gz"
tmp_file="$(mktemp)"
trap 'rm -f "${tmp_file}"' EXIT
curl --proto '=https' --tlsv1.2 -fsSL -o "${tmp_file}" "${source_url}"
source_sha="$(sha256_file "${tmp_file}")"

mkdir -p "$(dirname "${output_path}")"
cat > "${output_path}" <<RUBY
class Si < Formula
  desc "AI-first CLI for orchestrating coding agents and provider operations"
  homepage "https://github.com/${repo}"
  url "${source_url}"
  sha256 "${source_sha}"
  license "AGPL-3.0-only"
  head "https://github.com/${repo}.git", branch: "main"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w"), "./tools/si"
  end

  test do
    output = shell_output("#{bin}/si version")
    assert_match "si version", output
  end
end
RUBY

echo "rendered ${output_path}"
