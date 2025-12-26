#!/usr/bin/env bash
set -euo pipefail

# Bootstrap a new app workspace with docs, UI targets stub, and optional DB.
# Usage: start-app-project.sh <app-name> [options]

if [[ $# -lt 1 ]]; then
  echo "usage: start-app-project.sh <app-name> [--no-db] [--db-port <host_port>] [--web-path <path>] [--backend-path <path>] [--infra-path <path>] [--content-path <path>] [--kind <kind>] [--status <status>] [--web-stack <stack>] [--backend-stack <stack>] [--language <lang>] [--ui <ui>] [--runtime <runtime>] [--db <db>] [--orm <orm>]" >&2
  exit 1
fi

APP="$1"
shift

CREATE_DB=true
DB_PORT=""
WEB_PATH="web"
BACKEND_PATH=""
INFRA_PATH="infra"
CONTENT_PATH=""
KIND="saas"
STATUS="idea"
STACK_WEB="sveltekit"
STACK_BACKEND=""
STACK_LANGUAGE="typescript"
STACK_UI="shadcn-svelte"
STACK_RUNTIME="node"
DB_KIND="postgres"
DB_ORM="drizzle"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-db) CREATE_DB=false ;;
    --db-port)
      shift
      DB_PORT="${1:-}"
      ;;
    --web-path)
      shift
      WEB_PATH="${1:-}"
      ;;
    --backend-path)
      shift
      BACKEND_PATH="${1:-}"
      ;;
    --infra-path)
      shift
      INFRA_PATH="${1:-}"
      ;;
    --content-path)
      shift
      CONTENT_PATH="${1:-}"
      ;;
    --kind)
      shift
      KIND="${1:-$KIND}"
      ;;
    --status)
      shift
      STATUS="${1:-$STATUS}"
      ;;
    --web-stack)
      shift
      STACK_WEB="${1:-$STACK_WEB}"
      ;;
    --backend-stack)
      shift
      STACK_BACKEND="${1:-$STACK_BACKEND}"
      ;;
    --language)
      shift
      STACK_LANGUAGE="${1:-$STACK_LANGUAGE}"
      ;;
    --ui)
      shift
      STACK_UI="${1:-$STACK_UI}"
      ;;
    --runtime)
      shift
      STACK_RUNTIME="${1:-$STACK_RUNTIME}"
      ;;
    --db)
      shift
      DB_KIND="${1:-$DB_KIND}"
      ;;
    --orm)
      shift
      DB_ORM="${1:-$DB_ORM}"
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
  mkdir -p "$APP_DIR"
  echo "Created app directory $APP_DIR"
fi

mkdir -p "$APP_DIR"/{docs,ui-tests,.artifacts/visual,migrations}

if [[ -n "$WEB_PATH" && "$WEB_PATH" != "." ]]; then
  mkdir -p "$APP_DIR/$WEB_PATH"
fi
if [[ -n "$BACKEND_PATH" && "$BACKEND_PATH" != "." ]]; then
  mkdir -p "$APP_DIR/$BACKEND_PATH"
fi
if [[ -n "$INFRA_PATH" && "$INFRA_PATH" != "." ]]; then
  mkdir -p "$APP_DIR/$INFRA_PATH"
fi
if [[ -n "$CONTENT_PATH" && "$CONTENT_PATH" != "." ]]; then
  mkdir -p "$APP_DIR/$CONTENT_PATH"
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
  "kind": "${KIND}",
  "stack": {
    "web": "${STACK_WEB}",
    "backend": "${STACK_BACKEND}",
    "language": "${STACK_LANGUAGE}",
    "ui": "${STACK_UI}",
    "runtime": "${STACK_RUNTIME}"
  },
  "paths": {
    "web": "${WEB_PATH}",
    "backend": "${BACKEND_PATH}",
    "infra": "${INFRA_PATH}",
    "content": "${CONTENT_PATH}"
  },
  "modules": [],
  "owners": {
    "department": "",
    "dyad": ""
  },
  "data": {
    "db": "${DB_KIND}",
    "orm": "${DB_ORM}",
    "cache": ""
  },
  "integrations": [],
  "status": "${STATUS}"
}
EOF
  echo "Initialized ${APP_META}"
fi

# DB provisioning
if [[ "$CREATE_DB" == "true" && "$DB_KIND" == "postgres" ]]; then
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
if [[ -n "$WEB_PATH" ]]; then
  if [[ "$WEB_PATH" == "." ]]; then
    echo "- Add SvelteKit app under ${APP_DIR} (use create-svelte)."
  else
    echo "- Add SvelteKit app under ${APP_DIR}/${WEB_PATH} (use create-svelte)."
  fi
fi
echo "- Run bin/qa-visual.sh ${APP} to capture baseline once UI is available."
if [[ "$DB_KIND" == "postgres" ]]; then
  echo "- Use db creds in secrets/db-${APP}.env"
fi
