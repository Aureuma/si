package main

import "testing"

func TestBuildPaasAgentRuntimeAdapterPlanReady(t *testing.T) {
	prevRequire := requirePaasAgentCodexProfileFn
	prevAuth := codexProfileAuthStatusFn
	t.Cleanup(func() {
		requirePaasAgentCodexProfileFn = prevRequire
		codexProfileAuthStatusFn = prevAuth
	})
	requirePaasAgentCodexProfileFn = func(key string) (codexProfile, error) {
		return codexProfile{ID: "weekly", Email: "ops@example.com"}, nil
	}
	codexProfileAuthStatusFn = func(profile codexProfile) codexAuthCacheStatus {
		return codexAuthCacheStatus{Path: "/tmp/codex-auth.json", Exists: true}
	}

	incident := paasIncidentQueueEntry{
		Key: "key-1",
		Incident: paasIncidentEvent{
			ID:      "inc-1",
			Message: "deploy failed",
			Target:  "edge-a",
		},
	}
	plan, err := buildPaasAgentRuntimeAdapterPlan(paasAgentConfig{Name: "ops-agent", Profile: "weekly"}, &incident)
	if err != nil {
		t.Fatalf("build runtime plan: %v", err)
	}
	if !plan.Ready || plan.ProfileID != "weekly" || plan.AuthPath != "/tmp/codex-auth.json" || plan.IncidentID != "inc-1" {
		t.Fatalf("unexpected runtime plan: %#v", plan)
	}
}

func TestBuildPaasAgentRuntimeAdapterPlanMissingAuth(t *testing.T) {
	prevRequire := requirePaasAgentCodexProfileFn
	prevAuth := codexProfileAuthStatusFn
	t.Cleanup(func() {
		requirePaasAgentCodexProfileFn = prevRequire
		codexProfileAuthStatusFn = prevAuth
	})
	requirePaasAgentCodexProfileFn = func(key string) (codexProfile, error) {
		return codexProfile{ID: "weekly", Email: "ops@example.com"}, nil
	}
	codexProfileAuthStatusFn = func(profile codexProfile) codexAuthCacheStatus {
		return codexAuthCacheStatus{Path: "/tmp/codex-auth.json", Exists: false, Reason: "auth cache not found"}
	}

	plan, err := buildPaasAgentRuntimeAdapterPlan(paasAgentConfig{Name: "ops-agent", Profile: "weekly"}, nil)
	if err != nil {
		t.Fatalf("build runtime plan: %v", err)
	}
	if plan.Ready {
		t.Fatalf("expected non-ready plan when auth is missing: %#v", plan)
	}
	if plan.ProfileID != "weekly" || plan.AuthPath != "/tmp/codex-auth.json" || plan.Reason == "" {
		t.Fatalf("unexpected missing-auth plan: %#v", plan)
	}
}
