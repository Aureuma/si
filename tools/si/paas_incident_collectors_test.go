package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCollectPaasIncidentEventsFromCollectors(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	contextDir := filepath.Join(stateRoot, "contexts", defaultPaasContext)
	eventsDir := filepath.Join(contextDir, "events")
	if err := os.MkdirAll(eventsDir, 0o700); err != nil {
		t.Fatalf("mkdir events dir: %v", err)
	}

	mustWriteJSONL(t, filepath.Join(eventsDir, "deployments.jsonl"), []map[string]any{
		{
			"timestamp": "2026-02-17T14:30:00Z",
			"source":    "deploy",
			"command":   "deploy apply",
			"status":    "failed",
			"target":    "edge-a",
			"message":   "deploy failed",
		},
	})
	mustWriteJSONL(t, filepath.Join(eventsDir, "alerts.jsonl"), []map[string]any{
		{
			"timestamp": "2026-02-17T14:31:00Z",
			"source":    "alert",
			"command":   "alert ingress-tls",
			"status":    "retrying",
			"severity":  "warning",
			"target":    "edge-b",
			"message":   "acme challenge retrying",
		},
	})
	mustWriteJSONL(t, filepath.Join(eventsDir, "audit.jsonl"), []map[string]any{
		{
			"timestamp": "2026-02-17T14:32:00Z",
			"source":    "audit",
			"command":   "deploy reconcile",
			"status":    "failed",
			"severity":  "critical",
			"target":    "edge-c",
			"message":   "runtime unhealthy",
		},
	})

	events, stats, err := collectPaasIncidentEvents(20)
	if err != nil {
		t.Fatalf("collect incidents: %v", err)
	}
	if len(events) < 3 {
		t.Fatalf("expected >=3 incidents, got %#v", events)
	}

	categories := map[string]int{}
	for _, row := range events {
		categories[row.Category]++
	}
	if categories[paasIncidentCategoryDeploy] == 0 || categories[paasIncidentCategoryHealth] == 0 || categories[paasIncidentCategoryRuntime] == 0 {
		t.Fatalf("expected deploy/health/runtime categories, got %#v", categories)
	}

	statsByName := map[string]int{}
	for _, row := range stats {
		statsByName[strings.TrimSpace(row.Name)] = row.Count
	}
	if statsByName[paasIncidentCollectorDeployHook] == 0 || statsByName[paasIncidentCollectorHealthPoll] == 0 || statsByName[paasIncidentCollectorRuntime] == 0 {
		t.Fatalf("expected non-zero collector stats, got %#v", statsByName)
	}
}

func TestCollectPaasIncidentEventsDedupeWithinWindow(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	contextDir := filepath.Join(stateRoot, "contexts", defaultPaasContext)
	eventsDir := filepath.Join(contextDir, "events")
	if err := os.MkdirAll(eventsDir, 0o700); err != nil {
		t.Fatalf("mkdir events dir: %v", err)
	}

	mustWriteJSONL(t, filepath.Join(eventsDir, "deployments.jsonl"), []map[string]any{
		{
			"timestamp": "2026-02-17T14:31:00Z",
			"source":    "deploy",
			"command":   "deploy apply",
			"status":    "failed",
			"target":    "edge-a",
			"message":   "deploy failed first",
		},
		{
			"timestamp": "2026-02-17T14:33:00Z",
			"source":    "deploy",
			"command":   "deploy apply",
			"status":    "failed",
			"target":    "edge-a",
			"message":   "deploy failed second",
		},
	})

	events, stats, err := collectPaasIncidentEvents(20)
	if err != nil {
		t.Fatalf("collect incidents: %v", err)
	}
	deployCount := 0
	for _, row := range events {
		if row.Category == paasIncidentCategoryDeploy {
			deployCount++
		}
	}
	if deployCount != 1 {
		t.Fatalf("expected deduped deploy incident count=1, got %d rows=%#v", deployCount, events)
	}
	statsByName := map[string]int{}
	for _, row := range stats {
		statsByName[strings.TrimSpace(row.Name)] = row.Count
	}
	if statsByName[paasIncidentCollectorDeployHook] != 1 {
		t.Fatalf("expected deploy collector stat=1 after dedupe, got %#v", statsByName)
	}
}

func mustWriteJSONL(t *testing.T, path string, rows []map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer file.Close()
	for i, row := range rows {
		if strings.TrimSpace(toPaasString(row["timestamp"])) == "" {
			row["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
		}
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatalf("marshal row %d for %s: %v", i, path, err)
		}
		if _, err := file.Write(append(raw, '\n')); err != nil {
			t.Fatalf("write row %d for %s: %v", i, path, err)
		}
	}
}

