#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: spawn-dyad.sh <name> [role] [department]" >&2
  exit 1
fi

NAME="$1"
ROLE="${2:-generic}"
DEPT="${3:-$ROLE}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MANAGER_URL="${MANAGER_URL:-http://silexa-manager:9090}"

ACTOR_IMAGE="${ACTOR_IMAGE:-silexa/actor:local}"
CRITIC_IMAGE="${CRITIC_IMAGE:-silexa/critic:local}"

CODEX_MODEL="${CODEX_MODEL:-gpt-5.1-codex-max}"
CODEX_ACTOR_EFFORT="${CODEX_ACTOR_EFFORT:-}"
CODEX_CRITIC_EFFORT="${CODEX_CRITIC_EFFORT:-}"
SILEXA_REPO_URL="${SILEXA_REPO_URL:-}"
SILEXA_REPO_REF="${SILEXA_REPO_REF:-main}"

TELEGRAM_NOTIFY_URL="${TELEGRAM_NOTIFY_URL:-http://silexa-telegram-bot:8081/notify}"

if ! [[ "$NAME" =~ ^[A-Za-z0-9_-]+$ ]]; then
  echo "invalid dyad name: $NAME (allowed: letters, numbers, _ and -)" >&2
  exit 1
fi

case "${ROLE}" in
  infra|research)
    CODEX_ACTOR_EFFORT="${CODEX_ACTOR_EFFORT:-xhigh}"
    CODEX_CRITIC_EFFORT="${CODEX_CRITIC_EFFORT:-high}"
    ;;
  program_manager|pm)
    CODEX_ACTOR_EFFORT="${CODEX_ACTOR_EFFORT:-low}"
    CODEX_CRITIC_EFFORT="${CODEX_CRITIC_EFFORT:-xhigh}"
    ;;
  webdev|web)
    CODEX_ACTOR_EFFORT="${CODEX_ACTOR_EFFORT:-high}"
    CODEX_CRITIC_EFFORT="${CODEX_CRITIC_EFFORT:-high}"
    ;;
  *)
    CODEX_ACTOR_EFFORT="${CODEX_ACTOR_EFFORT:-high}"
    CODEX_CRITIC_EFFORT="${CODEX_CRITIC_EFFORT:-medium}"
    ;;
esac

if [[ -z "$MANAGER_URL" ]]; then
  echo "MANAGER_URL is required to verify dyad registration" >&2
  exit 1
fi

# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required to spawn dyads" >&2
  exit 1
fi

# Ensure registry entry exists.
MANAGER_URL="$MANAGER_URL" "${ROOT_DIR}/bin/register-dyad.sh" "$NAME" "$ROLE" "$DEPT" >/dev/null

cat <<EOF | kube apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: codex-${NAME}
  labels:
    app: silexa-dyad
    silexa.dyad: "${NAME}"
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 2Gi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: silexa-dyad-${NAME}
  labels:
    app: silexa-dyad
    silexa.dyad: "${NAME}"
    silexa.role: "${ROLE}"
    silexa.department: "${DEPT}"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: silexa-dyad
      silexa.dyad: "${NAME}"
  template:
    metadata:
      labels:
        app: silexa-dyad
        silexa.dyad: "${NAME}"
        silexa.role: "${ROLE}"
        silexa.department: "${DEPT}"
    spec:
      serviceAccountName: silexa-dyad
      volumes:
        - name: codex
          persistentVolumeClaim:
            claimName: codex-${NAME}
        - name: workspace
          emptyDir: {}
        - name: configs
          configMap:
            name: silexa-configs
            items:
              - key: codex-mcp-config.toml
                path: codex-mcp-config.toml
              - key: codex_accounts.json
                path: codex_accounts.json
              - key: router_rules.json
                path: router_rules.json
              - key: dyad_roster.json
                path: dyad_roster.json
              - key: ssh_target
                path: ssh_target
              - key: programs-web_hosting.json
                path: programs/web_hosting.json
              - key: programs-releaseparty.json
                path: programs/releaseparty/releaseparty.json
      initContainers:
        - name: repo-sync
          image: alpine/git:2.45.2
          env:
            - name: SILEXA_REPO_URL
              value: "${SILEXA_REPO_URL}"
            - name: SILEXA_REPO_REF
              value: "${SILEXA_REPO_REF}"
          command:
            - sh
            - -lc
            - |
              if [ -z "\${SILEXA_REPO_URL}" ]; then
                echo "SILEXA_REPO_URL not set; skipping repo sync"
                exit 0
              fi
              if [ ! -d /workspace/silexa/.git ]; then
                git clone --branch "\${SILEXA_REPO_REF}" "\${SILEXA_REPO_URL}" /workspace/silexa
              else
                cd /workspace/silexa
                git fetch origin "\${SILEXA_REPO_REF}" || true
                git checkout "\${SILEXA_REPO_REF}" || true
                git pull --ff-only origin "\${SILEXA_REPO_REF}" || true
              fi
          volumeMounts:
            - name: workspace
              mountPath: /workspace
      containers:
        - name: actor
          image: ${ACTOR_IMAGE}
          workingDir: /workspace/silexa/apps
          env:
            - name: ROLE
              value: "${ROLE}"
            - name: DEPARTMENT
              value: "${DEPT}"
            - name: DYAD_NAME
              value: "${NAME}"
            - name: DYAD_MEMBER
              value: "actor"
            - name: CODEX_INIT_FORCE
              value: "1"
            - name: CODEX_MODEL
              value: "${CODEX_MODEL}"
            - name: CODEX_REASONING_EFFORT
              value: "${CODEX_ACTOR_EFFORT}"
          volumeMounts:
            - name: codex
              mountPath: /root/.codex
            - name: workspace
              mountPath: /workspace
          command: ["bash","-lc","npm i -g @openai/codex >/dev/null 2>&1 || true; /workspace/silexa/bin/codex-init.sh >/proc/1/fd/1 2>/proc/1/fd/2 || true; exec tail -f /dev/null"]
        - name: critic
          image: ${CRITIC_IMAGE}
          env:
            - name: MANAGER_URL
              value: "${MANAGER_URL}"
            - name: TELEGRAM_NOTIFY_URL
              value: "${TELEGRAM_NOTIFY_URL}"
            - name: TELEGRAM_CHAT_ID
              value: "${TELEGRAM_CHAT_ID:-}"
            - name: DEPARTMENT
              value: "${DEPT}"
            - name: ROLE
              value: "${ROLE}"
            - name: DYAD_NAME
              value: "${NAME}"
            - name: DYAD_MEMBER
              value: "critic"
            - name: ACTOR_CONTAINER
              value: "actor"
            - name: CODEX_MODEL
              value: "${CODEX_MODEL}"
            - name: CODEX_REASONING_EFFORT
              value: "${CODEX_CRITIC_EFFORT}"
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
          volumeMounts:
            - name: configs
              mountPath: /configs
            - name: codex
              mountPath: /root/.codex
            - name: workspace
              mountPath: /workspace
EOF

echo "dyad ${NAME} deployed (namespace $(k8s_namespace))"
