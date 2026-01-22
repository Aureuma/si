package main

func capabilityText(role string) (string, bool) {
	switch role {
	case "actor-web":
		return `Role: actor-web
Model tier: code-tier (web/services), medium reasoning
Tools: git, docker, curl, MCP gateway (catalog), image builder (docker), per-app DB DSN
Guardrails: no infra/secrets changes; request via manager if needed`, true
	case "actor-infra":
		return `Role: actor-infra
Model tier: reasoning-tier (infra aware)
Tools: Pulumi/Terraform, docker, MCP gateway for infra servers
Guardrails: cost/dry-run required; no applies without pre-deploy check`, true
	case "actor-research":
		return `Role: actor-research
Model tier: reasoning-tier
Tools: HTTP clients, notebooks/spikes, MCP gateway for data sources
Guardrails: mock secrets; hand off for productionization`, true
	case "critic-web":
		return `Role: critic-web
Model tier: code-tier (review focus)
Tools: git diff, tests, curl-based smoke checks, MCP gateway if needed
Guardrails: flag issues; no broad rewrites`, true
	case "critic-infra":
		return `Role: critic-infra
Model tier: reasoning-tier
Tools: plan/apply diffs, cost checks, Pulumi/Terraform validation
Guardrails: block without dry-run; ensure least privilege`, true
	case "critic-qa":
		return `Role: critic-qa
Model tier: code-tier (test focus)
Tools: curl, Go test runners, MCP gateway optional
Guardrails: enforce smoke checks before approval; no prod secrets`, true
	case "critic-research":
		return `Role: critic-research
Model tier: reasoning-tier
Tools: analysis, benchmarks, MCP data sources
Guardrails: verify assumptions; avoid production changes`, true
	default:
		return "", false
	}
}
