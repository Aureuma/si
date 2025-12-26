#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<USAGE
usage: dyad-roster-apply.sh [--file <path>] [--spawn] [--dry-run]

Reads a dyad roster JSON and registers/updates dyads in the manager.
Use --spawn to start dyads with spawn=true.
USAGE
}

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
ROSTER_FILE="${DYAD_ROSTER_FILE:-$ROOT_DIR/configs/dyad_roster.json}"
MANAGER_URL="${MANAGER_URL:-http://localhost:9090}"
SPAWN=false
DRY_RUN=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --file)
      shift
      ROSTER_FILE="${1:-}"
      ;;
    --spawn)
      SPAWN=true
      ;;
    --dry-run)
      DRY_RUN=true
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown flag: $1" >&2
      usage
      exit 1
      ;;
  esac
  shift || true
done

if [[ ! -f "$ROSTER_FILE" ]]; then
  echo "missing roster file: ${ROSTER_FILE}" >&2
  exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required" >&2
  exit 1
fi
if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 is required" >&2
  exit 1
fi

if [[ "$DRY_RUN" == "true" ]]; then
  echo "dry run; no changes will be applied"
fi

while IFS=$'\t' read -r name role dept spawn payload_b64; do
  payload=$(python3 - <<'PY' "$payload_b64"
import base64, sys
print(base64.b64decode(sys.argv[1]).decode("utf-8"))
PY
  )

  if [[ "$DRY_RUN" == "true" ]]; then
    echo "would update dyad: $name"
    echo "$payload"
  else
    curl -fsSL -X POST -H "Content-Type: application/json" -d "$payload" "${MANAGER_URL%/}/dyads" >/dev/null
    echo "updated dyad: $name"
  fi

  if [[ "$SPAWN" == "true" && "$spawn" == "1" ]]; then
    if [[ "$DRY_RUN" == "true" ]]; then
      echo "would spawn dyad: $name"
    else
      MANAGER_URL="$MANAGER_URL" "${ROOT_DIR}/bin/spawn-dyad.sh" "$name" "$role" "$dept"
    fi
  fi
  echo
done < <(python3 - "$ROSTER_FILE" <<'PY'
import base64
import json
import sys

path = sys.argv[1]
with open(path, "r", encoding="utf-8") as fh:
    data = json.load(fh)

def get_bool(val, default=False):
    if isinstance(val, bool):
        return val
    return default

defaults = data.get("defaults", {}) if isinstance(data, dict) else {}
entries = data.get("dyads", []) if isinstance(data, dict) else []

for entry in entries:
    if not isinstance(entry, dict):
        continue
    name = str(entry.get("name", "")).strip()
    if not name:
        continue
    role = str(entry.get("role", "")).strip() or str(defaults.get("role", "")).strip() or "generic"
    dept = str(entry.get("department", "")).strip() or str(defaults.get("department", "")).strip() or role
    team = str(entry.get("team", "")).strip() or str(defaults.get("team", "")).strip()
    assignment = str(entry.get("assignment", "")).strip() or str(defaults.get("assignment", "")).strip()
    status = str(entry.get("status", "")).strip() or str(defaults.get("status", "")).strip()
    message = str(entry.get("message", "")).strip() or str(defaults.get("message", "")).strip()
    available = get_bool(entry.get("available", defaults.get("available", True)), True)
    tags = entry.get("tags", defaults.get("tags", []))
    if tags is None:
        tags = []
    if not isinstance(tags, list):
        tags = [str(tags)]

    payload = {
        "dyad": name,
        "role": role,
        "department": dept,
        "available": available,
    }
    if team:
        payload["team"] = team
    if assignment:
        payload["assignment"] = assignment
    if status:
        payload["status"] = status
    if message:
        payload["message"] = message
    if tags is not None:
        payload["tags"] = tags

    spawn = get_bool(entry.get("spawn", False), False)
    payload_b64 = base64.b64encode(json.dumps(payload).encode("utf-8")).decode("utf-8")
    print("\t".join([name, role, dept, "1" if spawn else "0", payload_b64]))
PY
)
