#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Validate release tag format and parity with tools/si/version.go.

Usage:
  tools/release/validate-release-version.sh --tag <vX.Y.Z[-suffix]>
EOF
}

die() {
  echo "error: $*" >&2
  exit 1
}

tag=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag)
      tag="${2:-}"
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

[[ -n "${tag}" ]] || die "--tag is required"
if [[ ! "${tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  die "tag must match vX.Y.Z (optionally with a prerelease/build suffix), got: ${tag}"
fi

actual="$(sed -n 's/^const siVersion = "\(.*\)"$/\1/p' tools/si/version.go)"
[[ -n "${actual}" ]] || die "could not parse tools/si/version.go"

if [[ "${actual}" != "${tag}" ]]; then
  die "tools/si/version.go has ${actual}, but release tag is ${tag}"
fi

echo "release tag and tools/si/version.go are aligned (${tag})"
