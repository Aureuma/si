#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

if command -v rg >/dev/null 2>&1; then
  mapfile -t files < <(rg --files -g 'app.json' "$ROOT_DIR/apps" | sort)
else
  mapfile -t files < <(find "$ROOT_DIR/apps" -name app.json -print | sort)
fi

if [[ ${#files[@]} -eq 0 ]]; then
  echo "no app.json files found under apps/"
  exit 0
fi

python3 - <<'PY' "${files[@]}"
import json
import sys

rows = []
for path in sys.argv[1:]:
    try:
        with open(path, "r", encoding="utf-8") as fh:
            data = json.load(fh)
        rows.append({
            "path": path,
            "name": data.get("name", ""),
            "kind": data.get("kind", ""),
            "status": data.get("status", "")
        })
    except Exception:
        rows.append({"path": path, "name": "", "kind": "", "status": ""})

width_name = max(len(r["name"]) for r in rows + [{"name": "name"}])
width_kind = max(len(r["kind"]) for r in rows + [{"kind": "kind"}])
width_status = max(len(r["status"]) for r in rows + [{"status": "status"}])

header = f"{'name'.ljust(width_name)}  {'kind'.ljust(width_kind)}  {'status'.ljust(width_status)}  path"
print(header)
print("-" * len(header))
for r in rows:
    name = r["name"].ljust(width_name)
    kind = r["kind"].ljust(width_kind)
    status = r["status"].ljust(width_status)
    print(f"{name}  {kind}  {status}  {r['path']}")
PY
