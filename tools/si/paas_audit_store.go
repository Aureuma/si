package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type paasAuditEntry struct {
	Timestamp string            `json:"timestamp"`
	Context   string            `json:"context"`
	Source    string            `json:"source"`
	Command   string            `json:"command"`
	Status    string            `json:"status"`
	Mode      string            `json:"mode"`
	Severity  string            `json:"severity,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`

	ErrorCode        string `json:"error_code,omitempty"`
	ErrorStage       string `json:"error_stage,omitempty"`
	ErrorTarget      string `json:"error_target,omitempty"`
	ErrorMessage     string `json:"error_message,omitempty"`
	ErrorRemediation string `json:"error_remediation,omitempty"`
}

func resolvePaasAuditLogPath(contextName string) (string, error) {
	contextDir, err := resolvePaasContextDir(contextName)
	if err != nil {
		return "", err
	}
	return filepath.Join(contextDir, "events", "audit.jsonl"), nil
}

func recordPaasAuditEvent(command, status, mode string, fields map[string]string, opErr error) string {
	path, err := resolvePaasAuditLogPath(currentPaasContext())
	if err != nil {
		return ""
	}
	entry := paasAuditEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Context:   currentPaasContext(),
		Source:    "audit",
		Command:   strings.TrimSpace(command),
		Status:    strings.ToLower(strings.TrimSpace(status)),
		Mode:      strings.TrimSpace(mode),
		Severity:  "info",
		Fields:    redactPaasSensitiveFields(fields),
	}
	if entry.Command == "" {
		return ""
	}
	if entry.Status == "" {
		entry.Status = "succeeded"
	}
	if entry.Mode == "" {
		entry.Mode = "live"
	}
	if entry.Status == "failed" {
		entry.Severity = "critical"
	}
	if opErr != nil {
		failure := asPaasOperationFailure(opErr)
		entry.ErrorCode = strings.TrimSpace(failure.Code)
		entry.ErrorStage = strings.TrimSpace(failure.Stage)
		entry.ErrorTarget = strings.TrimSpace(failure.Target)
		entry.ErrorMessage = strings.TrimSpace(errString(failure.Err))
		entry.ErrorRemediation = strings.TrimSpace(failure.Remediation)
		if entry.ErrorMessage != "" {
			entry.Severity = "critical"
		}
	}
	if len(entry.Fields) == 0 {
		entry.Fields = nil
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return ""
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return ""
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return ""
	}
	defer file.Close()
	if _, err := file.Write(append(raw, '\n')); err != nil {
		return ""
	}
	return path
}
