#!/usr/bin/env bash
set -euo pipefail

# Print recommended model/tier and tools for a dyad role.
# Usage: dyad-capability.sh <role>
# Roles: actor-web, actor-infra, actor-research, critic-web, critic-infra, critic-qa, critic-research

if [[ $# -lt 1 ]]; then
  echo "usage: dyad-capability.sh <role>" >&2
  exit 1
fi

role="$1"

case "$role" in
  actor-web)
    cat <<'EOF'
Role: actor-web
Model tier: code-tier (strong JS/TS/React), medium reasoning
Tools: git, node/pnpm, playwright, MCP gateway (catalog), image builder (buildctl), per-app DB DSN
Guardrails: no infra/secrets changes; request via manager if needed
EOF
    ;;
  actor-infra)
    cat <<'EOF'
Role: actor-infra
Model tier: reasoning-tier (infra aware)
Tools: Pulumi/Terraform, kubectl/helm, image builder (buildctl), MCP gateway for infra servers
Guardrails: cost/dry-run required; no applies without pre-deploy check
EOF
    ;;
  actor-research)
    cat <<'EOF'
Role: actor-research
Model tier: reasoning-tier
Tools: HTTP clients, notebooks/spikes, MCP gateway for data sources
Guardrails: mock secrets; hand off for productionization
EOF
    ;;
  critic-web)
    cat <<'EOF'
Role: critic-web
Model tier: code-tier (review focus)
Tools: git diff, tests, playwright visual QA, MCP gateway if needed
Guardrails: flag issues; no broad rewrites
EOF
    ;;
  critic-infra)
    cat <<'EOF'
Role: critic-infra
Model tier: reasoning-tier
Tools: plan/apply diffs, cost checks, Pulumi/Terraform validation
Guardrails: block without dry-run; ensure least privilege
EOF
    ;;
  critic-qa)
    cat <<'EOF'
Role: critic-qa
Model tier: code-tier (test focus)
Tools: Playwright, curl, test runners, MCP gateway optional
Guardrails: enforce smoke/visual before approval; no prod secrets
EOF
    ;;
  critic-research)
    cat <<'EOF'
Role: critic-research
Model tier: reasoning-tier
Tools: analysis, benchmarks, MCP data sources
Guardrails: verify assumptions; avoid production changes
EOF
    ;;
  *)
    echo "unknown role: $role" >&2
    exit 1
    ;;
esac
