package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	paasRemediationActionAutoAllow        = "auto-allow"
	paasRemediationActionApprovalRequired = "approval-required"
	paasRemediationActionDeny             = "deny"
)

type paasRemediationPolicy struct {
	DefaultAction string            `json:"default_action"`
	Severity      map[string]string `json:"severity,omitempty"`
	UpdatedAt     string            `json:"updated_at,omitempty"`
}

func resolvePaasRemediationPolicyPath(contextName string) (string, error) {
	contextDir, err := resolvePaasContextDir(contextName)
	if err != nil {
		return "", err
	}
	return filepath.Join(contextDir, "agents", "remediation_policy.json"), nil
}

func defaultPaasRemediationPolicy() paasRemediationPolicy {
	return paasRemediationPolicy{
		DefaultAction: paasRemediationActionApprovalRequired,
		Severity: map[string]string{
			paasIncidentSeverityInfo:     paasRemediationActionAutoAllow,
			paasIncidentSeverityWarning:  paasRemediationActionApprovalRequired,
			paasIncidentSeverityCritical: paasRemediationActionApprovalRequired,
		},
	}
}

func normalizePaasRemediationAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case paasRemediationActionAutoAllow, "auto", "allow":
		return paasRemediationActionAutoAllow
	case paasRemediationActionApprovalRequired, "approval", "approve":
		return paasRemediationActionApprovalRequired
	case paasRemediationActionDeny, "block":
		return paasRemediationActionDeny
	default:
		return ""
	}
}

func loadPaasRemediationPolicy(contextName string) (paasRemediationPolicy, string, error) {
	path, err := resolvePaasRemediationPolicyPath(contextName)
	if err != nil {
		return paasRemediationPolicy{}, "", err
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- context-scoped path.
	if err != nil {
		if os.IsNotExist(err) {
			return defaultPaasRemediationPolicy(), path, nil
		}
		return paasRemediationPolicy{}, path, err
	}
	var row paasRemediationPolicy
	if err := json.Unmarshal(raw, &row); err != nil {
		return paasRemediationPolicy{}, path, fmt.Errorf("invalid remediation policy: %w", err)
	}
	normalized := defaultPaasRemediationPolicy()
	if action := normalizePaasRemediationAction(row.DefaultAction); action != "" {
		normalized.DefaultAction = action
	}
	for severity, action := range row.Severity {
		sev := normalizePaasIncidentSeverity(severity)
		a := normalizePaasRemediationAction(action)
		if sev == "" || a == "" {
			continue
		}
		normalized.Severity[sev] = a
	}
	normalized.UpdatedAt = strings.TrimSpace(row.UpdatedAt)
	return normalized, path, nil
}

func savePaasRemediationPolicy(contextName string, policy paasRemediationPolicy) (string, error) {
	path, err := resolvePaasRemediationPolicyPath(contextName)
	if err != nil {
		return "", err
	}
	row := defaultPaasRemediationPolicy()
	if action := normalizePaasRemediationAction(policy.DefaultAction); action != "" {
		row.DefaultAction = action
	}
	for severity, action := range policy.Severity {
		sev := normalizePaasIncidentSeverity(severity)
		a := normalizePaasRemediationAction(action)
		if sev == "" || a == "" {
			return "", fmt.Errorf("invalid remediation policy override severity=%q action=%q", strings.TrimSpace(severity), strings.TrimSpace(action))
		}
		row.Severity[sev] = a
	}
	row.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	raw, err := json.MarshalIndent(row, "", "  ")
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func evaluatePaasRemediationPolicy(policy paasRemediationPolicy, incident *paasIncidentQueueEntry) (string, string) {
	normalized := defaultPaasRemediationPolicy()
	if action := normalizePaasRemediationAction(policy.DefaultAction); action != "" {
		normalized.DefaultAction = action
	}
	for severity, action := range policy.Severity {
		sev := normalizePaasIncidentSeverity(severity)
		a := normalizePaasRemediationAction(action)
		if sev == "" || a == "" {
			continue
		}
		normalized.Severity[sev] = a
	}
	severity := paasIncidentSeverityInfo
	if incident != nil {
		severity = normalizePaasIncidentSeverity(incident.Incident.Severity)
	}
	action := normalizePaasRemediationAction(normalized.Severity[severity])
	reason := "severity override"
	if action == "" {
		action = normalized.DefaultAction
		reason = "default action"
	}
	if action == "" {
		action = paasRemediationActionApprovalRequired
		reason = "fallback action"
	}
	return action, reason
}
