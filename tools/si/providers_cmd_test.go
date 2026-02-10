package main

import (
	"encoding/json"
	"testing"
)

func TestProvidersCharacteristicsCommandJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	stdout, stderr, err := runSICommand(t, map[string]string{}, "providers", "characteristics", "--provider", "github", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json parse failed: %v\nstdout=%s", err, stdout)
	}
	policy, ok := payload["policy"].(map[string]any)
	if !ok {
		t.Fatalf("missing policy section: %#v", payload)
	}
	if policy["defaults"] != "built_in_go" {
		t.Fatalf("unexpected defaults policy: %#v", policy)
	}
	providersRaw, ok := payload["providers"].([]any)
	if !ok || len(providersRaw) != 1 {
		t.Fatalf("unexpected providers payload: %#v", payload)
	}
	entry, ok := providersRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected provider entry: %#v", providersRaw[0])
	}
	if entry["provider"] != "github" {
		t.Fatalf("unexpected provider id: %#v", entry)
	}
	if entry["rate_limit_per_second"] == nil {
		t.Fatalf("missing provider rate: %#v", entry)
	}
	if _, ok := entry["capabilities"].(map[string]any); !ok {
		t.Fatalf("missing capabilities block: %#v", entry)
	}
}

func TestProvidersHealthCommandJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	stdout, stderr, err := runSICommand(t, map[string]string{}, "providers", "health", "--provider", "github", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json parse failed: %v\nstdout=%s", err, stdout)
	}
	if _, ok := payload["entries"].([]any); !ok {
		t.Fatalf("expected entries array payload, got: %#v", payload)
	}
	if _, ok := payload["guardrails"].([]any); !ok {
		t.Fatalf("expected guardrails array payload, got: %#v", payload)
	}
}
