package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func appendPaasDeployEvent(event map[string]any) (string, error) {
	contextDir, err := resolvePaasContextDir(currentPaasContext())
	if err != nil {
		return "", err
	}
	eventsDir := filepath.Join(contextDir, "events")
	if err := os.MkdirAll(eventsDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(eventsDir, "deployments.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if event == nil {
		event = map[string]any{}
	}
	if _, ok := event["timestamp"]; !ok {
		event["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if _, ok := event["context"]; !ok {
		event["context"] = currentPaasContext()
	}
	if _, ok := event["source"]; !ok {
		event["source"] = "si paas"
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')
	if _, err := f.Write(raw); err != nil {
		return "", err
	}
	return path, nil
}

func recordPaasDeployEvent(command, status string, fields map[string]string, opErr error) string {
	event := map[string]any{
		"command": strings.TrimSpace(command),
		"status":  strings.TrimSpace(status),
	}
	for key, value := range fields {
		if strings.TrimSpace(key) == "" {
			continue
		}
		event[key] = strings.TrimSpace(value)
	}
	if opErr != nil {
		failure := asPaasOperationFailure(opErr)
		event["error_code"] = strings.TrimSpace(failure.Code)
		event["error_stage"] = strings.TrimSpace(failure.Stage)
		event["error_target"] = strings.TrimSpace(failure.Target)
		event["error_message"] = errString(failure.Err)
		event["error_remediation"] = strings.TrimSpace(failure.Remediation)
	}
	path, err := appendPaasDeployEvent(event)
	if err != nil {
		return ""
	}
	return path
}
