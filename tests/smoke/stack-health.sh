#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck source=bin/swarm-lib.sh
source "${ROOT_DIR}/bin/swarm-lib.sh"

STACK=$(swarm_stack_name)

if ! docker stack services "$STACK" >/dev/null 2>&1; then
  echo "stack '$STACK' not found; deploy with bin/swarm-deploy.sh" >&2
  exit 1
fi

TMP_FILE=$(mktemp)
cleanup() { rm -f "$TMP_FILE"; }
trap cleanup EXIT

docker stack services "$STACK" --format '{{.Name}} {{.Replicas}}' > "$TMP_FILE"

python3 - <<'PY' "$TMP_FILE"
import sys

path = sys.argv[1]
lines = [line.strip() for line in open(path, "r", encoding="utf-8") if line.strip()]
if not lines:
    print("no services found in stack")
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
    print("stack replicas not ready:")
    for name, replicas in bad:
        print(f"- {name}: {replicas}")
    sys.exit(1)

print("stack replicas healthy")
PY

"${ROOT_DIR}/bin/health-report.sh" >/dev/null
