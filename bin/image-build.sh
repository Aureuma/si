#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: image-build.sh -t <tag> [-f <Dockerfile>] [--build-arg KEY=VALUE] [--push] <context>

Requires buildctl + buildkitd. Set BUILDKIT_HOST if buildkitd is remote.
Set SILEXA_IMAGE_PUSH=1 to push by default.
EOF
}

if [[ $# -lt 2 ]]; then
  usage >&2
  exit 1
fi

TAG=""
DOCKERFILE=""
CONTEXT=""
PUSH="${SILEXA_IMAGE_PUSH:-0}"
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
    --push)
      PUSH=1
      shift
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

PUSH_FLAG="false"
if [[ "$PUSH" == "1" || "$PUSH" == "true" ]]; then
  PUSH_FLAG="true"
fi

buildctl build \
  --frontend dockerfile.v0 \
  --local context="$CONTEXT_DIR" \
  --local dockerfile="$DOCKERFILE_DIR" \
  --opt filename="$DOCKERFILE_NAME" \
  "${BUILD_OPTS[@]}" \
  --output "type=image,name=$TAG,push=$PUSH_FLAG"
