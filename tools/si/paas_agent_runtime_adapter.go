package main

import (
	"fmt"
	"strings"
)

const paasAgentRuntimeAdapterModeCodexProfileAuth = "codex-profile-auth"

type paasAgentRuntimeAdapterPlan struct {
	Mode         string `json:"mode"`
	ProfileID    string `json:"profile_id"`
	ProfileEmail string `json:"profile_email,omitempty"`
	AuthPath     string `json:"auth_path"`
	Prompt       string `json:"prompt"`
	IncidentID   string `json:"incident_id,omitempty"`
	Ready        bool   `json:"ready"`
	Reason       string `json:"reason,omitempty"`
}

var (
	requirePaasAgentCodexProfileFn = requireCodexProfile
	codexProfileAuthStatusFn       = codexProfileAuthStatus
)

func buildPaasAgentRuntimeAdapterPlan(agent paasAgentConfig, incident *paasIncidentQueueEntry) (paasAgentRuntimeAdapterPlan, error) {
	profileKey := strings.TrimSpace(agent.Profile)
	if profileKey == "" {
		profiles := codexProfiles()
		if len(profiles) == 0 {
			return paasAgentRuntimeAdapterPlan{}, fmt.Errorf("no codex profiles configured for agent runtime adapter")
		}
		profileKey = strings.TrimSpace(profiles[0].ID)
	}
	profile, err := requirePaasAgentCodexProfileFn(profileKey)
	if err != nil {
		return paasAgentRuntimeAdapterPlan{}, err
	}
	auth := codexProfileAuthStatusFn(profile)
	if !auth.Exists {
		reason := strings.TrimSpace(auth.Reason)
		if reason == "" {
			reason = "codex auth cache unavailable"
		}
		return paasAgentRuntimeAdapterPlan{
			Mode:         paasAgentRuntimeAdapterModeCodexProfileAuth,
			ProfileID:    strings.TrimSpace(profile.ID),
			ProfileEmail: strings.TrimSpace(profile.Email),
			AuthPath:     strings.TrimSpace(auth.Path),
			Prompt:       buildPaasAgentIncidentPrompt(agent.Name, incident),
			IncidentID:   resolvePaasAgentIncidentID(incident),
			Ready:        false,
			Reason:       reason,
		}, nil
	}
	return paasAgentRuntimeAdapterPlan{
		Mode:         paasAgentRuntimeAdapterModeCodexProfileAuth,
		ProfileID:    strings.TrimSpace(profile.ID),
		ProfileEmail: strings.TrimSpace(profile.Email),
		AuthPath:     strings.TrimSpace(auth.Path),
		Prompt:       buildPaasAgentIncidentPrompt(agent.Name, incident),
		IncidentID:   resolvePaasAgentIncidentID(incident),
		Ready:        true,
	}, nil
}

func buildPaasAgentIncidentPrompt(agentName string, incident *paasIncidentQueueEntry) string {
	name := strings.TrimSpace(agentName)
	if name == "" {
		name = "paas-agent"
	}
	if incident == nil {
		return fmt.Sprintf("%s: no active incident selected; validate queue state and emit noop result", name)
	}
	message := strings.TrimSpace(incident.Incident.Message)
	if message == "" {
		message = "incident detected"
	}
	target := strings.TrimSpace(incident.Incident.Target)
	if target == "" {
		target = "unknown-target"
	}
	return fmt.Sprintf("%s: analyze incident %s for target %s and propose safe remediation steps: %s",
		name,
		resolvePaasAgentIncidentID(incident),
		target,
		message,
	)
}

func resolvePaasAgentIncidentID(incident *paasIncidentQueueEntry) string {
	if incident == nil {
		return ""
	}
	id := strings.TrimSpace(incident.Incident.ID)
	if id != "" {
		return id
	}
	return strings.TrimSpace(incident.Key)
}
