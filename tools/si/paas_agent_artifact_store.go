package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

type paasAgentRunArtifact struct {
	Timestamp string             `json:"timestamp"`
	Context   string             `json:"context"`
	Source    string             `json:"source"`
	Record    paasAgentRunRecord `json:"record"`
}

func resolvePaasAgentRunArtifactPath(contextName, runID, timestamp string) (string, error) {
	contextDir, err := resolvePaasContextDir(contextName)
	if err != nil {
		return "", err
	}
	runKey := sanitizePaasAgentArtifactToken(runID)
	if runKey == "" {
		runKey = "run"
	}
	timeKey := sanitizePaasAgentArtifactToken(timestamp)
	if timeKey == "" {
		timeKey = "event"
	}
	return filepath.Join(contextDir, "events", "agent-artifacts", runKey, timeKey+".json"), nil
}

func savePaasAgentRunArtifact(contextName string, record paasAgentRunRecord) (string, error) {
	path, err := resolvePaasAgentRunArtifactPath(contextName, record.RunID, record.Timestamp)
	if err != nil {
		return "", err
	}
	row := record
	row.ArtifactPath = strings.TrimSpace(row.ArtifactPath)
	if row.ArtifactPath == "" {
		row.ArtifactPath = path
	}
	payload := paasAgentRunArtifact{
		Timestamp: strings.TrimSpace(row.Timestamp),
		Context:   strings.TrimSpace(contextName),
		Source:    "agent-run",
		Record:    row,
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
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

func sanitizePaasAgentArtifactToken(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	sanitized := strings.TrimSpace(b.String())
	sanitized = strings.Trim(sanitized, "._-")
	if sanitized == "" {
		return ""
	}
	if len(sanitized) > 128 {
		return sanitized[:128]
	}
	return sanitized
}

func loadPaasAgentRunArtifact(path string) (paasAgentRunArtifact, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- artifact path is context-scoped.
	if err != nil {
		return paasAgentRunArtifact{}, err
	}
	var row paasAgentRunArtifact
	if err := json.Unmarshal(raw, &row); err != nil {
		return paasAgentRunArtifact{}, fmt.Errorf("invalid agent run artifact: %w", err)
	}
	return row, nil
}
