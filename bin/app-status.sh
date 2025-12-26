#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: app-status.sh <app-name> [--stack-name <name>]" >&2
  exit 1
fi

APP="$1"
shift

STACK_NAME=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --stack-name)
      shift
      STACK_NAME="${1:-}"
      ;;
    *)
      echo "unknown flag: $1" >&2
      exit 1
      ;;
  esac
  shift || true
done

if [[ -z "$STACK_NAME" ]]; then
  STACK_NAME="silexa-app-${APP}"
fi

if ! docker stack services "$STACK_NAME" >/dev/null 2>&1; then
  echo "stack ${STACK_NAME} not found" >&2
  exit 1
fi

TMP_FILE=$(mktemp)
cleanup() { rm -f "$TMP_FILE"; }
trap cleanup EXIT

docker stack services "$STACK_NAME" --format '{{.Name}} {{.Replicas}}' > "$TMP_FILE"

python3 - <<'PY' "$TMP_FILE" "$STACK_NAME"
import sys

path = sys.argv[1]
stack = sys.argv[2]
lines = [line.strip() for line in open(path, "r", encoding="utf-8") if line.strip()]
if not lines:
    print(f"no services found in {stack}")
    sys.exit(1)

bad = []
for line in lines:
    parts = line.split(None, 1)
    if len(parts) != 2:
        bad.append((line, "unparsed"))
        continue
    name, replicas = parts
    if "/" not in replicas:
        bad.append((name, replicas))
        continue
    cur, desired = replicas.split("/", 1)
    if cur != desired:
        bad.append((name, replicas))

if bad:
    print(f"{stack} replicas not ready:")
    for name, replicas in bad:
        print(f"- {name}: {replicas}")
    sys.exit(1)

print(f"{stack} replicas healthy")
PY
