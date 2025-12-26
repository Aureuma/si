#!/usr/bin/env bash
set -euo pipefail

MODE=""
if [[ "${1:-}" == "--strict" ]]; then
  MODE="--strict"
fi

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

if command -v rg >/dev/null 2>&1; then
  mapfile -t files < <(rg --files -g 'app.json' "$ROOT_DIR/apps" | sort)
else
  mapfile -t files < <(find "$ROOT_DIR/apps" -name app.json -print | sort)
fi

if [[ ${#files[@]} -eq 0 ]]; then
  echo "no app.json files found under apps/" >&2
  exit 1
fi

failed=0
for meta in "${files[@]}"; do
  app_dir=$(dirname "$meta")
  app_name=$(basename "$app_dir")
  echo "==> validate ${app_name}"
  if ! "${ROOT_DIR}/bin/app-validate.sh" "$app_name" $MODE; then
    failed=1
  fi
  echo
done

exit $failed
