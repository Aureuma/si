package main

import (
	"fmt"
	"sort"
	"strings"
)

const (
	paasIncidentCollectorDeployHook = "deploy-hook"
	paasIncidentCollectorHealthPoll = "health-poll"
	paasIncidentCollectorRuntime    = "runtime-watch"
)

type paasIncidentCollectorStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func collectPaasIncidentEvents(limit int) ([]paasIncidentEvent, []paasIncidentCollectorStat, error) {
	if limit < 1 {
		limit = 1
	}
	scanLimit := limit * 6
	if scanLimit < 200 {
		scanLimit = 200
	}
	rows, _, err := loadPaasEventRecords(scanLimit, "", "")
	if err != nil {
		return nil, nil, err
	}
	events := make([]paasIncidentEvent, 0, limit)
	seen := make(map[string]struct{})
	counts := map[string]int{
		paasIncidentCollectorDeployHook: 0,
		paasIncidentCollectorHealthPoll: 0,
		paasIncidentCollectorRuntime:    0,
	}
	for _, row := range rows {
		candidates := collectIncidentCandidatesFromPaasEventRow(row)
		for _, event := range candidates {
			key := strings.TrimSpace(event.DedupeKey) + "|" + strings.TrimSpace(event.WindowStart)
			if key == "|" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			events = append(events, event)
			counts[event.Source] = counts[event.Source] + 1
			if len(events) >= limit {
				break
			}
		}
		if len(events) >= limit {
			break
		}
	}
	stats := []paasIncidentCollectorStat{
		{Name: paasIncidentCollectorDeployHook, Count: counts[paasIncidentCollectorDeployHook]},
		{Name: paasIncidentCollectorHealthPoll, Count: counts[paasIncidentCollectorHealthPoll]},
		{Name: paasIncidentCollectorRuntime, Count: counts[paasIncidentCollectorRuntime]},
	}
	return events, stats, nil
}

func collectIncidentCandidatesFromPaasEventRow(row paasEventRecord) []paasIncidentEvent {
	events := make([]paasIncidentEvent, 0, 3)
	if event, ok := incidentFromDeployHookEvent(row); ok {
		events = append(events, event)
	}
	if event, ok := incidentFromHealthPollEvent(row); ok {
		events = append(events, event)
	}
	if event, ok := incidentFromRuntimeEvent(row); ok {
		events = append(events, event)
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].TriggeredAt > events[j].TriggeredAt
	})
	return events
}

func incidentFromDeployHookEvent(row paasEventRecord) (paasIncidentEvent, bool) {
	if !strings.EqualFold(strings.TrimSpace(row.Source), "deploy") {
		return paasIncidentEvent{}, false
	}
	if !isPaasIncidentCandidateStatus(row.Status) {
		return paasIncidentEvent{}, false
	}
	severity := normalizePaasIncidentSeverity(row.Severity)
	if severity == paasIncidentSeverityInfo {
		severity = inferPaasIncidentSeverityFromStatus(row.Status)
	}
	return newPaasIncidentEvent(paasIncidentEventInput{
		Source:      paasIncidentCollectorDeployHook,
		Category:    paasIncidentCategoryDeploy,
		Severity:    severity,
		Message:     fallbackPaasIncidentMessage(row.Message, row.Command, row.Status),
		Target:      strings.TrimSpace(row.Target),
		Signal:      normalizePaasIncidentSignal(row.Command, row.Status),
		TriggeredAt: parsePaasEventTimestamp(row.Timestamp),
		DedupeHint:  strings.TrimSpace(row.Command),
		Metadata: map[string]string{
			"command": strings.TrimSpace(row.Command),
			"status":  strings.ToLower(strings.TrimSpace(row.Status)),
		},
	}), true
}

func incidentFromHealthPollEvent(row paasEventRecord) (paasIncidentEvent, bool) {
	command := strings.ToLower(strings.TrimSpace(row.Command))
	source := strings.ToLower(strings.TrimSpace(row.Source))
	if source != "alert" && !strings.Contains(command, "health") && !strings.Contains(command, "ingress") {
		return paasIncidentEvent{}, false
	}
	if !isPaasIncidentCandidateStatus(row.Status) && !isPaasIncidentCandidateSeverity(row.Severity) {
		return paasIncidentEvent{}, false
	}
	severity := normalizePaasIncidentSeverity(row.Severity)
	if severity == paasIncidentSeverityInfo {
		severity = inferPaasIncidentSeverityFromStatus(row.Status)
	}
	if severity == paasIncidentSeverityInfo {
		severity = paasIncidentSeverityWarning
	}
	return newPaasIncidentEvent(paasIncidentEventInput{
		Source:      paasIncidentCollectorHealthPoll,
		Category:    paasIncidentCategoryHealth,
		Severity:    severity,
		Message:     fallbackPaasIncidentMessage(row.Message, row.Command, row.Status),
		Target:      strings.TrimSpace(row.Target),
		Signal:      normalizePaasIncidentSignal(row.Command, row.Status),
		TriggeredAt: parsePaasEventTimestamp(row.Timestamp),
		DedupeHint:  strings.TrimSpace(row.Command),
		Metadata: map[string]string{
			"command": strings.TrimSpace(row.Command),
			"status":  strings.ToLower(strings.TrimSpace(row.Status)),
		},
	}), true
}

func incidentFromRuntimeEvent(row paasEventRecord) (paasIncidentEvent, bool) {
	if !strings.EqualFold(strings.TrimSpace(row.Source), "audit") {
		return paasIncidentEvent{}, false
	}
	if !isPaasIncidentCandidateStatus(row.Status) {
		return paasIncidentEvent{}, false
	}
	severity := normalizePaasIncidentSeverity(row.Severity)
	if severity == paasIncidentSeverityInfo {
		severity = inferPaasIncidentSeverityFromStatus(row.Status)
	}
	return newPaasIncidentEvent(paasIncidentEventInput{
		Source:      paasIncidentCollectorRuntime,
		Category:    paasIncidentCategoryRuntime,
		Severity:    severity,
		Message:     fallbackPaasIncidentMessage(row.Message, row.Command, row.Status),
		Target:      strings.TrimSpace(row.Target),
		Signal:      normalizePaasIncidentSignal(row.Command, row.Status),
		TriggeredAt: parsePaasEventTimestamp(row.Timestamp),
		DedupeHint:  strings.TrimSpace(row.Command),
		Metadata: map[string]string{
			"command": strings.TrimSpace(row.Command),
			"status":  strings.ToLower(strings.TrimSpace(row.Status)),
		},
	}), true
}

func isPaasIncidentCandidateStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "error", "degraded", "retrying":
		return true
	default:
		return false
	}
}

func isPaasIncidentCandidateSeverity(severity string) bool {
	switch normalizePaasIncidentSeverity(severity) {
	case paasIncidentSeverityWarning, paasIncidentSeverityCritical:
		return true
	default:
		return false
	}
}

func inferPaasIncidentSeverityFromStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "error":
		return paasIncidentSeverityCritical
	case "degraded", "retrying":
		return paasIncidentSeverityWarning
	default:
		return paasIncidentSeverityInfo
	}
}

func normalizePaasIncidentSignal(command, status string) string {
	parts := []string{
		strings.ReplaceAll(strings.ToLower(strings.TrimSpace(command)), " ", "_"),
		strings.ReplaceAll(strings.ToLower(strings.TrimSpace(status)), " ", "_"),
	}
	joined := strings.Trim(strings.Join(parts, "_"), "_")
	if joined == "" {
		return "unknown"
	}
	return joined
}

func fallbackPaasIncidentMessage(message, command, status string) string {
	msg := strings.TrimSpace(message)
	if msg != "" {
		return msg
	}
	cmd := strings.TrimSpace(command)
	st := strings.TrimSpace(status)
	if cmd != "" && st != "" {
		return fmt.Sprintf("%s status=%s", cmd, st)
	}
	if cmd != "" {
		return cmd
	}
	if st != "" {
		return "status=" + st
	}
	return "incident candidate"
}

