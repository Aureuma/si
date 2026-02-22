package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	paasApprovalDecisionApproved = "approved"
	paasApprovalDecisionDenied   = "denied"
)

type paasAgentApprovalDecision struct {
	RunID     string `json:"run_id"`
	Agent     string `json:"agent"`
	Decision  string `json:"decision"`
	Note      string `json:"note,omitempty"`
	Actor     string `json:"actor,omitempty"`
	Source    string `json:"source,omitempty"`
	Timestamp string `json:"timestamp"`
}

type paasAgentApprovalStore struct {
	Decisions []paasAgentApprovalDecision `json:"decisions,omitempty"`
}

func resolvePaasAgentApprovalStorePath(contextName string) (string, error) {
	contextDir, err := resolvePaasContextDir(contextName)
	if err != nil {
		return "", err
	}
	return filepath.Join(contextDir, "agents", "approvals.json"), nil
}

func loadPaasAgentApprovalStore(contextName string) (paasAgentApprovalStore, string, error) {
	path, err := resolvePaasAgentApprovalStorePath(contextName)
	if err != nil {
		return paasAgentApprovalStore{}, "", err
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- context-scoped path.
	if err != nil {
		if os.IsNotExist(err) {
			return paasAgentApprovalStore{}, path, nil
		}
		return paasAgentApprovalStore{}, path, err
	}
	var store paasAgentApprovalStore
	if err := json.Unmarshal(raw, &store); err != nil {
		return paasAgentApprovalStore{}, path, fmt.Errorf("invalid approval store: %w", err)
	}
	sortPaasAgentApprovalDecisions(store.Decisions)
	return store, path, nil
}

func savePaasAgentApprovalStore(contextName string, store paasAgentApprovalStore) (string, error) {
	path, err := resolvePaasAgentApprovalStorePath(contextName)
	if err != nil {
		return "", err
	}
	sortPaasAgentApprovalDecisions(store.Decisions)
	raw, err := json.MarshalIndent(store, "", "  ")
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

func upsertPaasAgentApprovalDecision(store paasAgentApprovalStore, row paasAgentApprovalDecision) paasAgentApprovalStore {
	row.RunID = strings.TrimSpace(row.RunID)
	row.Agent = strings.TrimSpace(row.Agent)
	row.Decision = normalizePaasApprovalDecision(row.Decision)
	row.Note = strings.TrimSpace(row.Note)
	row.Actor = strings.TrimSpace(row.Actor)
	row.Source = strings.TrimSpace(row.Source)
	row.Timestamp = strings.TrimSpace(row.Timestamp)
	if row.Timestamp == "" {
		row.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if row.RunID == "" || row.Decision == "" {
		return store
	}
	replaced := false
	for i := range store.Decisions {
		if strings.TrimSpace(store.Decisions[i].RunID) != row.RunID {
			continue
		}
		store.Decisions[i] = row
		replaced = true
		break
	}
	if !replaced {
		store.Decisions = append(store.Decisions, row)
	}
	sortPaasAgentApprovalDecisions(store.Decisions)
	return store
}

func normalizePaasApprovalDecision(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "approve", "approved", "allow":
		return paasApprovalDecisionApproved
	case "deny", "denied", "block":
		return paasApprovalDecisionDenied
	default:
		return ""
	}
}

func sortPaasAgentApprovalDecisions(rows []paasAgentApprovalDecision) {
	sort.SliceStable(rows, func(i, j int) bool {
		left := parsePaasIncidentQueueTimestamp(rows[i].Timestamp)
		right := parsePaasIncidentQueueTimestamp(rows[j].Timestamp)
		if left.Equal(right) {
			return rows[i].RunID < rows[j].RunID
		}
		return left.After(right)
	})
}

func notifyPaasAgentApprovalTelegramLinkage(runID, agentName, decision, note string) string {
	fields := map[string]string{
		"run":                    strings.TrimSpace(runID),
		"agent":                  strings.TrimSpace(agentName),
		"decision":               strings.TrimSpace(decision),
		"callback_agent_approve": "si paas agent approve --run " + quoteSingle(strings.TrimSpace(runID)),
		"callback_agent_deny":    "si paas agent deny --run " + quoteSingle(strings.TrimSpace(runID)),
	}
	if strings.TrimSpace(note) != "" {
		fields["note"] = strings.TrimSpace(note)
	}
	return emitPaasOperationalAlert(
		"agent approval",
		"info",
		"",
		fmt.Sprintf("run %s for agent %s marked %s", strings.TrimSpace(runID), strings.TrimSpace(agentName), strings.TrimSpace(decision)),
		"use callback commands to revise approval decision",
		fields,
	)
}
