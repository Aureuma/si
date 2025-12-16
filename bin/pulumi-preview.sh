#!/usr/bin/env bash
set -euo pipefail

STACK=${STACK:-dev}
PROJECT_DIR=${PROJECT_DIR:-/opt/silexa/pulumi/infra}

cd "$PROJECT_DIR"
pulumi stack select "$STACK" || pulumi stack init "$STACK"
pulumi preview
