#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: app-validate.sh <app-name> [--strict]" >&2
  exit 1
fi

APP="$1"
MODE="warn"
if [[ "${2:-}" == "--strict" ]]; then
  MODE="strict"
fi

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
APP_DIR="${ROOT_DIR}/apps/${APP}"
META_FILE="${APP_DIR}/app.json"

if [[ ! -f "$META_FILE" ]]; then
  echo "error: missing ${META_FILE}" >&2
  exit 1
fi

python3 - <<'PY' "$META_FILE" "$APP_DIR" "$APP" "$MODE" "$ROOT_DIR"
import json
import os
import sys

meta_path, app_dir, app_name, mode, root_dir = sys.argv[1:]
errors = []
warnings = []

try:
    with open(meta_path, "r", encoding="utf-8") as fh:
        meta = json.load(fh)
except Exception as exc:
    errors.append(f"invalid app.json: {exc}")
    meta = {}

name = meta.get("name", "")
if not name:
    errors.append("app.json missing name")

paths = meta.get("paths", {})
if not isinstance(paths, dict):
    errors.append("app.json paths must be object")
    paths = {}

web_path = paths.get("web", "") or ""
backend_path = paths.get("backend", "") or ""
infra_path = paths.get("infra", "") or ""

stack = meta.get("stack", {})
if not isinstance(stack, dict):
    stack = {}

web_stack = stack.get("web", "")

if web_path:
    if web_path == ".":
        web_dir = app_dir
    else:
        web_dir = os.path.join(app_dir, web_path)
    if not os.path.isdir(web_dir):
        errors.append(f"web path missing: {web_dir}")
    else:
        pkg = os.path.join(web_dir, "package.json")
        if not os.path.isfile(pkg):
            warnings.append(f"web package.json missing: {pkg}")
        svelte_config = None
        for name in ("svelte.config.js", "svelte.config.ts"):
            candidate = os.path.join(web_dir, name)
            if os.path.isfile(candidate):
                svelte_config = candidate
                break
        if web_stack == "sveltekit":
            if not svelte_config:
                warnings.append("svelte.config missing for SvelteKit app")
            else:
                try:
                    with open(svelte_config, "r", encoding="utf-8") as fh:
                        content = fh.read()
                    if "adapter-node" not in content:
                        warnings.append("SvelteKit adapter-node not referenced in svelte.config")
                except Exception as exc:
                    warnings.append(f"unable to read {svelte_config}: {exc}")

if backend_path:
    backend_dir = os.path.join(app_dir, backend_path)
    if not os.path.isdir(backend_dir):
        errors.append(f"backend path missing: {backend_dir}")
    else:
        dockerfile = os.path.join(backend_dir, "Dockerfile")
        if not os.path.isfile(dockerfile):
            warnings.append(f"backend Dockerfile missing: {dockerfile}")

if infra_path:
    infra_dir = os.path.join(app_dir, infra_path)
    stack_file = os.path.join(infra_dir, "stack.yml")
    if not os.path.isfile(stack_file):
        errors.append(f"infra stack.yml missing: {stack_file}")
    else:
        try:
            with open(stack_file, "r", encoding="utf-8") as fh:
                stack_content = fh.read()
            if f"app-{app_name}-env" not in stack_content:
                warnings.append("stack.yml does not reference app env secret")
        except Exception as exc:
            warnings.append(f"unable to read {stack_file}: {exc}")
else:
    warnings.append("paths.infra is empty; no stack.yml expected")

secret_file = os.path.join(root_dir, "secrets", f"app-{app_name}.env")
if not os.path.isfile(secret_file):
    warnings.append(f"missing secrets/app-{app_name}.env")

print(f"app {app_name} validation")
for item in errors:
    print(f"error: {item}")
for item in warnings:
    print(f"warn: {item}")

if errors:
    sys.exit(1)
if mode == "strict" and warnings:
    sys.exit(2)
PY
