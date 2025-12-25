#!/usr/bin/env bash
set -euo pipefail

# Bootstrap a new app workspace with docs, UI targets stub, and optional DB.
# Usage: start-app-project.sh <app-name> [--no-db] [--db-port <host_port>]

if [[ $# -lt 1 ]]; then
  echo "usage: start-app-project.sh <app-name> [--no-db] [--db-port <host_port>]" >&2
  exit 1
fi

APP="$1"
shift

CREATE_DB=true
DB_PORT=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-db) CREATE_DB=false ;;
    --db-port)
      shift
      DB_PORT="${1:-}"
      ;;
    *)
      echo "unknown flag: $1" >&2
      exit 1
      ;;
  esac
  shift || true
done

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
APP_DIR="${ROOT_DIR}/apps/${APP}"

if [[ -d "$APP_DIR" ]]; then
  echo "App directory already exists: $APP_DIR" >&2
else
  mkdir -p "$APP_DIR"/{docs,ui-tests,.artifacts/visual}
  echo "Created app directory $APP_DIR"
fi

# Plan template
PLAN_FILE="${APP_DIR}/docs/plan.md"
if [[ ! -f "$PLAN_FILE" ]]; then
  cat >"$PLAN_FILE" <<EOF
# ${APP} Plan

## Vision & outcomes
- What problem are we solving?
- Target users, success metrics, constraints (perf, compliance, budget).

## Scope & requirements
- User journeys
- Must-haves vs nice-to-haves
- Integrations (auth, payments, notifications)

## Team & dyads
- Planner:
- Builder:
- QA:
- Infra:
- Marketing:
- Creds:

## Architecture notes
- Frontend stack:
- Backend/API:
- Data model:
- External services:

## Testing
- Unit/integration:
- Visual (qa-visual):
- Accessibility:
- Smoke:

## Deployment & rollout
- Environments:
- Migration plan:
- Launch/rollout steps:

## Risks & mitigations
- ...

## Budget & cost guardrails
- ...
EOF
  echo "Initialized ${PLAN_FILE}"
fi

# UI targets stub
TARGETS_FILE="${APP_DIR}/ui-tests/targets.json"
if [[ ! -f "$TARGETS_FILE" ]]; then
  cat >"$TARGETS_FILE" <<'EOF'
{
  "baseURL": "http://localhost:3000",
  "routes": [
    { "path": "/", "name": "home", "waitFor": "body" }
  ],
  "viewports": [
    { "width": 1280, "height": 720, "name": "desktop" },
    { "width": 375, "height": 667, "name": "mobile" }
  ]
}
EOF
  echo "Initialized ${TARGETS_FILE}"
fi

# App metadata
APP_META="${APP_DIR}/app.json"
if [[ ! -f "$APP_META" ]]; then
  cat >"$APP_META" <<EOF
{
  "name": "${APP}",
  "kind": "saas",
  "stack": {
    "frontend": "nextjs",
    "backend": "nextjs"
  },
  "modules": [],
  "owners": {
    "department": "",
    "dyad": ""
  },
  "data": {
    "db": "postgres",
    "cache": ""
  },
  "integrations": [],
  "status": "idea"
}
EOF
  echo "Initialized ${APP_META}"
fi

# DB provisioning
if [[ "$CREATE_DB" == "true" ]]; then
  DB_CMD=("bin/app-db.sh" "create" "$APP")
  if [[ -n "$DB_PORT" ]]; then
    DB_CMD+=("$DB_PORT")
  fi
  echo "Provisioning per-app Postgres..."
  (cd "$ROOT_DIR" && "${DB_CMD[@]}")
fi

echo "App ${APP} bootstrap complete."
echo "Next steps:"
echo "- Fill ${PLAN_FILE}"
echo "- Run bin/qa-visual.sh ${APP} to capture baseline once UI is available."
echo "- Use db creds in secrets/db-${APP}.env"
