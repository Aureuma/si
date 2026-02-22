package main

import "testing"

func TestEvaluatePaasRemediationPolicyDefaults(t *testing.T) {
	policy := defaultPaasRemediationPolicy()
	infoAction, _ := evaluatePaasRemediationPolicy(policy, &paasIncidentQueueEntry{
		Incident: paasIncidentEvent{Severity: paasIncidentSeverityInfo},
	})
	warningAction, _ := evaluatePaasRemediationPolicy(policy, &paasIncidentQueueEntry{
		Incident: paasIncidentEvent{Severity: paasIncidentSeverityWarning},
	})
	criticalAction, _ := evaluatePaasRemediationPolicy(policy, &paasIncidentQueueEntry{
		Incident: paasIncidentEvent{Severity: paasIncidentSeverityCritical},
	})
	if infoAction != paasRemediationActionAutoAllow {
		t.Fatalf("expected info action auto-allow, got %q", infoAction)
	}
	if warningAction != paasRemediationActionApprovalRequired {
		t.Fatalf("expected warning action approval-required, got %q", warningAction)
	}
	if criticalAction != paasRemediationActionApprovalRequired {
		t.Fatalf("expected critical action approval-required, got %q", criticalAction)
	}
}

func TestEvaluatePaasRemediationPolicyOverrides(t *testing.T) {
	policy := paasRemediationPolicy{
		DefaultAction: paasRemediationActionDeny,
		Severity: map[string]string{
			paasIncidentSeverityCritical: paasRemediationActionAutoAllow,
		},
	}
	action, reason := evaluatePaasRemediationPolicy(policy, &paasIncidentQueueEntry{
		Incident: paasIncidentEvent{Severity: paasIncidentSeverityCritical},
	})
	if action != paasRemediationActionAutoAllow {
		t.Fatalf("expected override action auto-allow, got %q", action)
	}
	if reason == "" {
		t.Fatalf("expected evaluation reason to be set")
	}

	defaultAction, _ := evaluatePaasRemediationPolicy(policy, &paasIncidentQueueEntry{
		Incident: paasIncidentEvent{Severity: paasIncidentSeverityWarning},
	})
	if defaultAction != paasRemediationActionApprovalRequired {
		t.Fatalf("expected warning action to inherit normalized defaults, got %q", defaultAction)
	}
}
