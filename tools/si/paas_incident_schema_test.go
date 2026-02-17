package main

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizePaasIncidentSeverity(t *testing.T) {
	cases := map[string]string{
		"":         paasIncidentSeverityInfo,
		"info":     paasIncidentSeverityInfo,
		"warn":     paasIncidentSeverityWarning,
		"warning":  paasIncidentSeverityWarning,
		"error":    paasIncidentSeverityCritical,
		"critical": paasIncidentSeverityCritical,
		"fatal":    paasIncidentSeverityCritical,
		"other":    paasIncidentSeverityInfo,
	}
	for input, expected := range cases {
		got := normalizePaasIncidentSeverity(input)
		if got != expected {
			t.Fatalf("normalizePaasIncidentSeverity(%q)=%q want=%q", input, got, expected)
		}
	}
}

func TestBuildPaasIncidentDedupeKeyIsStable(t *testing.T) {
	a := buildPaasIncidentDedupeKey("default", "deploy-hook", "deploy", "critical", "edge-a", "health_failed", "app=billing")
	b := buildPaasIncidentDedupeKey("default", "deploy-hook", "deploy", "critical", "edge-a", "health_failed", "app=billing")
	c := buildPaasIncidentDedupeKey("default", "deploy-hook", "deploy", "critical", "edge-b", "health_failed", "app=billing")
	if a == "" || b == "" || c == "" {
		t.Fatalf("expected non-empty dedupe keys: a=%q b=%q c=%q", a, b, c)
	}
	if a != b {
		t.Fatalf("expected stable dedupe key for same input: a=%q b=%q", a, b)
	}
	if a == c {
		t.Fatalf("expected different dedupe key when target changes: a=%q c=%q", a, c)
	}
}

func TestResolvePaasIncidentDedupeWindow(t *testing.T) {
	ts := time.Date(2026, 2, 17, 14, 33, 41, 0, time.UTC)
	start, end := resolvePaasIncidentDedupeWindow(ts, 10*time.Minute)
	if start.Format(time.RFC3339) != "2026-02-17T14:30:00Z" {
		t.Fatalf("unexpected dedupe window start: %s", start.Format(time.RFC3339))
	}
	if end.Format(time.RFC3339) != "2026-02-17T14:40:00Z" {
		t.Fatalf("unexpected dedupe window end: %s", end.Format(time.RFC3339))
	}
}

func TestNewPaasIncidentEventDefaultsAndRedaction(t *testing.T) {
	event := newPaasIncidentEvent(paasIncidentEventInput{
		Context:      "internal-dogfood",
		Source:       "deploy-hook",
		Category:     "deploy",
		Severity:     "error",
		Message:      "health check failed",
		Target:       "edge-a",
		Signal:       "deploy_health_failed",
		TriggeredAt:  time.Date(2026, 2, 17, 14, 33, 41, 0, time.UTC),
		DedupeWindow: 5 * time.Minute,
		Metadata: map[string]string{
			"api_token": "abc",
			"summary":   "failure",
		},
	})
	if event.ID == "" || event.DedupeKey == "" || event.CorrelationID == "" {
		t.Fatalf("expected generated IDs/keys, got %#v", event)
	}
	if event.Severity != paasIncidentSeverityCritical {
		t.Fatalf("expected severity critical, got %#v", event)
	}
	if event.Status != paasIncidentStatusOpen {
		t.Fatalf("expected default status open, got %#v", event)
	}
	if !strings.Contains(event.ID, event.DedupeKey) {
		t.Fatalf("expected event id to include dedupe key suffix: %#v", event)
	}
	if event.Metadata["api_token"] != "<redacted>" {
		t.Fatalf("expected metadata redaction for api_token, got %#v", event.Metadata)
	}
	if event.Metadata["summary"] != "failure" {
		t.Fatalf("expected non-sensitive metadata unchanged, got %#v", event.Metadata)
	}
}

