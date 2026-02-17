package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type paasEventRecord struct {
	Timestamp string            `json:"timestamp"`
	Source    string            `json:"source"`
	Command   string            `json:"command"`
	Status    string            `json:"status,omitempty"`
	Severity  string            `json:"severity,omitempty"`
	Target    string            `json:"target,omitempty"`
	Message   string            `json:"message,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

func loadPaasEventRecords(limit int, severityFilter, statusFilter string) ([]paasEventRecord, []string, error) {
	contextDir, err := resolvePaasContextDir(currentPaasContext())
	if err != nil {
		return nil, nil, err
	}
	if limit < 1 {
		limit = 1
	}
	filterSeverity := strings.ToLower(strings.TrimSpace(severityFilter))
	filterStatus := strings.ToLower(strings.TrimSpace(statusFilter))

	paths := []string{
		filepath.Join(contextDir, "events", "deployments.jsonl"),
		filepath.Join(contextDir, "events", "alerts.jsonl"),
	}
	rows := make([]paasEventRecord, 0, limit)
	for _, path := range paths {
		loaded, err := loadPaasEventsFromPath(path)
		if err != nil {
			return nil, paths, err
		}
		rows = append(rows, loaded...)
	}

	filtered := make([]paasEventRecord, 0, len(rows))
	for _, row := range rows {
		if filterSeverity != "" && !strings.EqualFold(strings.TrimSpace(row.Severity), filterSeverity) {
			continue
		}
		if filterStatus != "" && !strings.EqualFold(strings.TrimSpace(row.Status), filterStatus) {
			continue
		}
		filtered = append(filtered, row)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		left := parsePaasEventTimestamp(filtered[i].Timestamp)
		right := parsePaasEventTimestamp(filtered[j].Timestamp)
		return left.After(right)
	})
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, paths, nil
}

func loadPaasEventsFromPath(path string) ([]paasEventRecord, error) {
	file, err := os.Open(path) // #nosec G304 -- context-scoped state path.
	if err != nil {
		if os.IsNotExist(err) {
			return []paasEventRecord{}, nil
		}
		return nil, err
	}
	defer file.Close()

	rows := []paasEventRecord{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		row, ok := parsePaasEventLine(line)
		if !ok {
			continue
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read events %s: %w", path, err)
	}
	return rows, nil
}

func parsePaasEventLine(line string) (paasEventRecord, bool) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return paasEventRecord{}, false
	}
	record := paasEventRecord{
		Timestamp: strings.TrimSpace(toPaasString(raw["timestamp"])),
		Source:    normalizePaasEventSource(toPaasString(raw["source"])),
		Command:   strings.TrimSpace(toPaasString(raw["command"])),
		Status:    strings.ToLower(strings.TrimSpace(toPaasString(raw["status"]))),
		Severity:  strings.ToLower(strings.TrimSpace(toPaasString(raw["severity"]))),
		Target:    strings.TrimSpace(toPaasString(raw["target"])),
		Message:   strings.TrimSpace(toPaasString(raw["message"])),
		Fields:    map[string]string{},
	}
	if record.Timestamp == "" {
		record.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if record.Source == "" {
		record.Source = "event"
	}
	if record.Command == "" {
		record.Command = record.Source
	}
	if record.Target == "" {
		record.Target = strings.TrimSpace(toPaasString(raw["error_target"]))
	}
	if record.Message == "" {
		record.Message = strings.TrimSpace(toPaasString(raw["error_message"]))
	}
	if record.Severity == "" {
		record.Severity = inferPaasEventSeverity(record.Status)
	}

	known := map[string]struct{}{
		"timestamp":     {},
		"context":       {},
		"source":        {},
		"command":       {},
		"status":        {},
		"severity":      {},
		"target":        {},
		"message":       {},
		"guidance":      {},
		"error_target":  {},
		"error_message": {},
		"fields":        {},
	}
	for key, value := range raw {
		if _, exists := known[key]; exists {
			continue
		}
		record.Fields[key] = strings.TrimSpace(toPaasString(value))
	}
	if nested, ok := raw["fields"].(map[string]any); ok {
		for key, value := range nested {
			record.Fields[key] = strings.TrimSpace(toPaasString(value))
		}
	}
	record.Fields = redactPaasSensitiveFields(record.Fields)
	if len(record.Fields) == 0 {
		record.Fields = nil
	}
	return record, true
}

func normalizePaasEventSource(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	switch v {
	case "", "si paas":
		return "event"
	case "alert", "deploy", "event":
		return v
	default:
		if strings.Contains(v, "alert") {
			return "alert"
		}
		if strings.Contains(v, "deploy") {
			return "deploy"
		}
		return v
	}
}

func inferPaasEventSeverity(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "error":
		return "critical"
	case "retrying", "degraded":
		return "warning"
	case "sent", "accepted":
		return "info"
	case "succeeded", "ok":
		return "info"
	default:
		return ""
	}
}

func parsePaasEventTimestamp(value string) time.Time {
	v := strings.TrimSpace(value)
	if v == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, v); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func toPaasString(value any) string {
	if value == nil {
		return ""
	}
	switch row := value.(type) {
	case string:
		return row
	case fmt.Stringer:
		return row.String()
	default:
		return fmt.Sprintf("%v", value)
	}
}
