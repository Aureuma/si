#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required" >&2
  exit 1
fi

TMP_FILE=$(mktemp)
cleanup() { rm -f "$TMP_FILE"; }
trap cleanup EXIT

kube get deployments --no-headers -o custom-columns=NAME:.metadata.name,READY:.status.readyReplicas,DESIRED:.spec.replicas > "$TMP_FILE"

python3 - <<'PY' "$TMP_FILE"
import sys

path = sys.argv[1]
lines = [line.strip() for line in open(path, "r", encoding="utf-8") if line.strip()]
if not lines:
    print("no deployments found")
    sys.exit(1)

bad = []
for line in lines:
    parts = line.split()
    if len(parts) < 3:
        bad.append((line, "unparsed"))
        continue
    name, ready, desired = parts[0], parts[1], parts[2]
    if ready != desired:
        bad.append((name, f"{ready}/{desired}"))

if bad:
    print("deployments not ready:")
    for name, replicas in bad:
        print(f"- {name}: {replicas}")
    sys.exit(1)

print("deployments healthy")
PY

"${ROOT_DIR}/bin/health-report.sh" >/dev/null
