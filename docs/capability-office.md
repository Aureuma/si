## Capability Office (model + tool selection for dyads)

Purpose: centralize decisions about model class, reasoning depth, and tool permissions per dyad, so teams stay consistent and budgets stay sane.

### Responsibilities
- Map roles/departments to recommended model tiers (e.g., code-oriented vs reasoning-heavy) and guardrails.
- Approve toolsets per dyad (CLI, MCP servers, secrets scope).
- Enforce caps: max context, rate, and expensive tools only when justified.
- Maintain profiles in `profiles/` and keep them aligned with current tasks.

### How to use
- Get recommendations: `bin/dyad-capability.sh <role>` (roles: actor-web, actor-infra, actor-research, critic-web, critic-infra, critic-qa, critic-research).
- For unusual tasks, log a feedback note with constraints (budget, latency, safety) and let the capability office update mappings.
- Record deviations in manager `/feedback` with reason and duration (e.g., “use high-depth model for incident for 1h”).

### Model tiers (suggested)
- **Code-tier**: optimized for code generation/review; use for builders/critics in web/backend.
- **Reasoning-tier**: higher-depth reasoning; use for research, complex infra planning, sensitive security reviews.
- **Light-tier**: cost-sensitive, use for routine status/triage.

### Tooling defaults
- Web: git, node/pnpm, playwright visual QA, MCP gateway for catalogs.
- Infra: Pulumi/Terraform, kubectl/helm, image builder (buildctl), MCP gateway for infra servers.
- Research: HTTP clients, notebooks/spikes, MCP registry for data sources.
- QA: Playwright, curl, test runners; no prod secrets.

### Escalation
- If a dyad needs a higher-tier model or extra tools, open an access request via manager `/access-requests` and notify security/creds. Document scope and sunset date.
