#!/usr/bin/env bash
set -euo pipefail

MANAGER_URL=${MANAGER_URL:-http://localhost:9090}

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required" >&2
  exit 1
fi
if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 is required" >&2
  exit 1
fi

DATA=$(curl -fsSL "${MANAGER_URL%/}/dyads")

python3 - <<'PY' "$DATA"
import json
import sys

raw = sys.argv[1]
try:
    data = json.loads(raw)
except Exception:
    print("invalid dyads payload")
    sys.exit(1)

rows = []
for d in data:
    rows.append({
        "dyad": d.get("dyad", ""),
        "team": d.get("team", ""),
        "assignment": d.get("assignment", ""),
        "role": d.get("role", ""),
        "dept": d.get("department", ""),
        "available": "yes" if d.get("available", False) else "no",
        "state": d.get("state", "")
    })

if not rows:
    print("no dyads registered")
    sys.exit(0)

widths = {
    "dyad": max(len(r["dyad"]) for r in rows + [{"dyad": "dyad"}]),
    "team": max(len(r["team"]) for r in rows + [{"team": "team"}]),
    "assignment": max(len(r["assignment"]) for r in rows + [{"assignment": "assignment"}]),
    "role": max(len(r["role"]) for r in rows + [{"role": "role"}]),
    "dept": max(len(r["dept"]) for r in rows + [{"dept": "dept"}]),
    "available": max(len(r["available"]) for r in rows + [{"available": "avail"}]),
    "state": max(len(r["state"]) for r in rows + [{"state": "state"}])
}

header = f"{'dyad'.ljust(widths['dyad'])}  {'team'.ljust(widths['team'])}  {'assignment'.ljust(widths['assignment'])}  {'role'.ljust(widths['role'])}  {'dept'.ljust(widths['dept'])}  {'avail'.ljust(widths['available'])}  {'state'.ljust(widths['state'])}"
print(header)
print("-" * len(header))
for r in rows:
    print(
        f"{r['dyad'].ljust(widths['dyad'])}  "
        f"{r['team'].ljust(widths['team'])}  "
        f"{r['assignment'].ljust(widths['assignment'])}  "
        f"{r['role'].ljust(widths['role'])}  "
        f"{r['dept'].ljust(widths['dept'])}  "
        f"{r['available'].ljust(widths['available'])}  "
        f"{r['state'].ljust(widths['state'])}"
    )
PY
