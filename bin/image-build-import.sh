#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: image-build-import.sh -t <tag> [-f <Dockerfile>] [--build-arg KEY=VALUE] <context>

Builds an OCI image with buildctl and imports it into k3s containerd.
Requires buildctl + buildkitd and k3s (ctr) on the host.
EOF
}

if [[ $# -lt 2 ]]; then
  usage >&2
  exit 1
fi

TAG=""
DOCKERFILE=""
CONTEXT=""
BUILD_ARGS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    -t|--tag)
      TAG="$2"
      shift 2
      ;;
    -f|--file)
      DOCKERFILE="$2"
      shift 2
      ;;
    --build-arg)
      BUILD_ARGS+=("$2")
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      break
      ;;
    *)
      if [[ -z "$CONTEXT" ]]; then
        CONTEXT="$1"
        shift
      else
        usage >&2
        exit 1
      fi
      ;;
  esac
done

if [[ -z "$TAG" || -z "$CONTEXT" ]]; then
  usage >&2
  exit 1
fi

if ! command -v buildctl >/dev/null 2>&1; then
  echo "buildctl not found. Install buildkit (buildctl + buildkitd) before building images." >&2
  exit 1
fi
if ! command -v k3s >/dev/null 2>&1; then
  echo "k3s not found. Install k3s before importing images." >&2
  exit 1
fi

if [[ -z "$DOCKERFILE" ]]; then
  DOCKERFILE="${CONTEXT%/}/Dockerfile"
fi

if [[ ! -f "$DOCKERFILE" ]]; then
  echo "Dockerfile not found: $DOCKERFILE" >&2
  exit 1
fi

DOCKERFILE_DIR=$(cd "$(dirname "$DOCKERFILE")" && pwd)
DOCKERFILE_NAME=$(basename "$DOCKERFILE")
CONTEXT_DIR=$(cd "$CONTEXT" && pwd)

BUILD_OPTS=()
for arg in "${BUILD_ARGS[@]}"; do
  BUILD_OPTS+=(--opt "build-arg:${arg}")
done

SUDO=""
if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  SUDO="sudo"
fi

tmp="$($SUDO mktemp /root/silexa-image-XXXX.tar)"
cleanup() {
  $SUDO rm -f "$tmp" >/dev/null 2>&1 || true
}
trap cleanup EXIT

$SUDO buildctl build \
  --frontend dockerfile.v0 \
  --local context="$CONTEXT_DIR" \
  --local dockerfile="$DOCKERFILE_DIR" \
  --opt filename="$DOCKERFILE_NAME" \
  "${BUILD_OPTS[@]}" \
  --output "type=oci,name=$TAG,dest=$tmp"

$SUDO k3s ctr images import "$tmp"

echo "imported $TAG into k3s containerd"
