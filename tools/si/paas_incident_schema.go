package main

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

const (
	paasIncidentSeverityInfo     = "info"
	paasIncidentSeverityWarning  = "warning"
	paasIncidentSeverityCritical = "critical"

	paasIncidentCategoryDeploy  = "deploy"
	paasIncidentCategoryHealth  = "health"
	paasIncidentCategoryRuntime = "runtime"
	paasIncidentCategoryWebhook = "webhook"
	paasIncidentCategoryAgent   = "agent"
	paasIncidentCategoryUnknown = "unknown"

	paasIncidentStatusOpen       = "open"
	paasIncidentStatusSuppressed = "suppressed"
	paasIncidentStatusResolved   = "resolved"

	paasIncidentDefaultDedupeWindow = 5 * time.Minute
)

type paasIncidentEvent struct {
	ID            string            `json:"id"`
	Context       string            `json:"context"`
	Source        string            `json:"source"`
	Category      string            `json:"category"`
	Severity      string            `json:"severity"`
	Status        string            `json:"status"`
	Message       string            `json:"message"`
	Target        string            `json:"target,omitempty"`
	Signal        string            `json:"signal,omitempty"`
	DedupeKey     string            `json:"dedupe_key"`
	CorrelationID string            `json:"correlation_id"`
	TriggeredAt   string            `json:"triggered_at"`
	WindowStart   string            `json:"window_start"`
	WindowEnd     string            `json:"window_end"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type paasIncidentEventInput struct {
	Context        string
	Source         string
	Category       string
	Severity       string
	Status         string
	Message        string
	Target         string
	Signal         string
	TriggeredAt    time.Time
	DedupeWindow   time.Duration
	CorrelationID  string
	Metadata       map[string]string
	DedupeHint     string
	ExternalRunRef string
}

func normalizePaasIncidentSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case paasIncidentSeverityCritical, "error", "fatal":
		return paasIncidentSeverityCritical
	case paasIncidentSeverityWarning, "warn":
		return paasIncidentSeverityWarning
	case paasIncidentSeverityInfo, "":
		return paasIncidentSeverityInfo
	default:
		return paasIncidentSeverityInfo
	}
}

func normalizePaasIncidentCategory(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case paasIncidentCategoryDeploy:
		return paasIncidentCategoryDeploy
	case paasIncidentCategoryHealth:
		return paasIncidentCategoryHealth
	case paasIncidentCategoryRuntime:
		return paasIncidentCategoryRuntime
	case paasIncidentCategoryWebhook:
		return paasIncidentCategoryWebhook
	case paasIncidentCategoryAgent:
		return paasIncidentCategoryAgent
	case "":
		return paasIncidentCategoryUnknown
	default:
		return paasIncidentCategoryUnknown
	}
}

func normalizePaasIncidentStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case paasIncidentStatusResolved:
		return paasIncidentStatusResolved
	case paasIncidentStatusSuppressed:
		return paasIncidentStatusSuppressed
	case "", paasIncidentStatusOpen:
		return paasIncidentStatusOpen
	default:
		return paasIncidentStatusOpen
	}
}

func resolvePaasIncidentDedupeWindow(triggeredAt time.Time, window time.Duration) (time.Time, time.Time) {
	if triggeredAt.IsZero() {
		triggeredAt = time.Now().UTC()
	}
	triggeredAt = triggeredAt.UTC()
	if window <= 0 {
		window = paasIncidentDefaultDedupeWindow
	}
	start := triggeredAt.Truncate(window)
	return start, start.Add(window)
}

func buildPaasIncidentDedupeKey(context, source, category, severity, target, signal, hint string) string {
	clean := []string{
		strings.ToLower(strings.TrimSpace(context)),
		strings.ToLower(strings.TrimSpace(source)),
		strings.ToLower(strings.TrimSpace(category)),
		strings.ToLower(strings.TrimSpace(severity)),
		strings.ToLower(strings.TrimSpace(target)),
		strings.ToLower(strings.TrimSpace(signal)),
		strings.ToLower(strings.TrimSpace(hint)),
	}
	payload := strings.Join(clean, "|")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:8])
}

func buildPaasIncidentCorrelationID(dedupeKey string, windowStart time.Time, externalRunRef string) string {
	parts := []string{
		strings.ToLower(strings.TrimSpace(dedupeKey)),
		windowStart.UTC().Format("20060102T1504"),
		strings.ToLower(strings.TrimSpace(externalRunRef)),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:8])
}

func newPaasIncidentEvent(input paasIncidentEventInput) paasIncidentEvent {
	triggeredAt := input.TriggeredAt
	if triggeredAt.IsZero() {
		triggeredAt = time.Now().UTC()
	}
	windowStart, windowEnd := resolvePaasIncidentDedupeWindow(triggeredAt, input.DedupeWindow)
	severity := normalizePaasIncidentSeverity(input.Severity)
	category := normalizePaasIncidentCategory(input.Category)
	source := strings.ToLower(strings.TrimSpace(input.Source))
	if source == "" {
		source = "event-bridge"
	}
	contextName := strings.TrimSpace(input.Context)
	if contextName == "" {
		contextName = currentPaasContext()
	}
	dedupeKey := buildPaasIncidentDedupeKey(
		contextName,
		source,
		category,
		severity,
		input.Target,
		input.Signal,
		input.DedupeHint,
	)
	correlationID := strings.TrimSpace(input.CorrelationID)
	if correlationID == "" {
		correlationID = buildPaasIncidentCorrelationID(dedupeKey, windowStart, input.ExternalRunRef)
	}
	now := triggeredAt.UTC()
	eventID := now.Format("20060102T150405.000000000Z07:00") + "-" + dedupeKey
	return paasIncidentEvent{
		ID:            eventID,
		Context:       contextName,
		Source:        source,
		Category:      category,
		Severity:      severity,
		Status:        normalizePaasIncidentStatus(input.Status),
		Message:       strings.TrimSpace(input.Message),
		Target:        strings.TrimSpace(input.Target),
		Signal:        strings.TrimSpace(input.Signal),
		DedupeKey:     dedupeKey,
		CorrelationID: correlationID,
		TriggeredAt:   now.Format(time.RFC3339Nano),
		WindowStart:   windowStart.Format(time.RFC3339Nano),
		WindowEnd:     windowEnd.Format(time.RFC3339Nano),
		Metadata:      redactPaasSensitiveFields(input.Metadata),
	}
}
