package main

import "sort"

var skillByRole = map[string]string{
	"actor-web": `Role: actor-web
Model tier: code-tier (web/services), medium reasoning
Tools: git, docker, curl, image builder (docker)
Guardrails: no infra or credential changes without approval`,
	"actor-infra": `Role: actor-infra
Model tier: reasoning-tier (infra aware)
Tools: Terraform, docker
Guardrails: cost/dry-run required; no applies without pre-deploy check`,
	"actor-research": `Role: actor-research
Model tier: reasoning-tier
Tools: HTTP clients, notebooks/spikes
Guardrails: mock external dependencies; hand off for productionization`,
	"critic-web": `Role: critic-web
Model tier: code-tier (review focus)
Tools: git diff, tests, curl-based smoke checks
Guardrails: flag issues; no broad rewrites`,
	"critic-infra": `Role: critic-infra
Model tier: reasoning-tier
Tools: plan/apply diffs, cost checks, Terraform validation
Guardrails: block without dry-run; ensure least privilege`,
	"critic-qa": `Role: critic-qa
Model tier: code-tier (test focus)
Tools: curl, Go test runners
Guardrails: enforce smoke checks before approval; no prod credentials`,
	"critic-research": `Role: critic-research
Model tier: reasoning-tier
Tools: analysis, benchmarks
Guardrails: verify assumptions; avoid production changes`,
}

func skillText(role string) (string, bool) {
	text, ok := skillByRole[role]
	return text, ok
}

func skillRoles() []string {
	roles := make([]string, 0, len(skillByRole))
	for role := range skillByRole {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	return roles
}
