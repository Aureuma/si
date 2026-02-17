package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpsertPaasIncidentQueueEntriesAndRetention(t *testing.T) {
	now := time.Date(2026, 2, 17, 15, 0, 0, 0, time.UTC)
	oldIncident := newPaasIncidentEvent(paasIncidentEventInput{
		Context:      defaultPaasContext,
		Source:       paasIncidentCollectorDeployHook,
		Category:     paasIncidentCategoryDeploy,
		Severity:     paasIncidentSeverityWarning,
		Message:      "old incident",
		Target:       "edge-a",
		Signal:       "deploy_apply_failed",
		TriggeredAt:  now.Add(-72 * time.Hour),
		DedupeWindow: 5 * time.Minute,
	})
	newIncident := newPaasIncidentEvent(paasIncidentEventInput{
		Context:      defaultPaasContext,
		Source:       paasIncidentCollectorDeployHook,
		Category:     paasIncidentCategoryDeploy,
		Severity:     paasIncidentSeverityCritical,
		Message:      "new incident",
		Target:       "edge-b",
		Signal:       "deploy_apply_failed",
		TriggeredAt:  now,
		DedupeWindow: 5 * time.Minute,
	})

	entries, inserted, updated := upsertPaasIncidentQueueEntries(nil, []paasIncidentEvent{oldIncident, newIncident}, now)
	if inserted != 2 || updated != 0 {
		t.Fatalf("unexpected upsert counts inserted=%d updated=%d entries=%#v", inserted, updated, entries)
	}
	retained, pruned := applyPaasIncidentQueueRetention(entries, 10, 24*time.Hour, now)
	if pruned != 1 {
		t.Fatalf("expected one entry pruned by age, got %d entries=%#v", pruned, retained)
	}
	if len(retained) != 1 || retained[0].Incident.Message != "new incident" {
		t.Fatalf("expected only newest incident retained, got %#v", retained)
	}
}

func TestSyncPaasIncidentQueueFromCollectors(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	contextDir := filepath.Join(stateRoot, "contexts", defaultPaasContext)
	eventsDir := filepath.Join(contextDir, "events")
	if err := os.MkdirAll(eventsDir, 0o700); err != nil {
		t.Fatalf("mkdir events dir: %v", err)
	}
	mustWriteIncidentQueueJSONL(t, filepath.Join(eventsDir, "deployments.jsonl"), []map[string]any{
		{
			"timestamp": "2026-02-17T16:00:00Z",
			"source":    "deploy",
			"command":   "deploy apply",
			"status":    "failed",
			"target":    "edge-a",
			"message":   "deploy failed",
		},
	})
	mustWriteIncidentQueueJSONL(t, filepath.Join(eventsDir, "alerts.jsonl"), []map[string]any{
		{
			"timestamp": "2026-02-17T16:01:00Z",
			"source":    "alert",
			"command":   "alert ingress-tls",
			"status":    "retrying",
			"severity":  "warning",
			"target":    "edge-a",
			"message":   "acme retrying",
		},
	})
	mustWriteIncidentQueueJSONL(t, filepath.Join(eventsDir, "audit.jsonl"), []map[string]any{
		{
			"timestamp": "2026-02-17T16:02:00Z",
			"source":    "audit",
			"command":   "deploy reconcile",
			"status":    "failed",
			"severity":  "critical",
			"target":    "edge-a",
			"message":   "runtime unhealthy",
		},
	})

	result, err := syncPaasIncidentQueueFromCollectors(20, 100, 24*time.Hour)
	if err != nil {
		t.Fatalf("sync incident queue: %v", err)
	}
	if result.Collected == 0 || result.Inserted == 0 || result.Total == 0 {
		t.Fatalf("unexpected sync result: %#v", result)
	}
	rows, _, err := loadPaasIncidentQueueSummary(20)
	if err != nil {
		t.Fatalf("load queue summary: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("expected queue rows after sync")
	}
}

func mustWriteIncidentQueueJSONL(t *testing.T, path string, rows []map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer file.Close()
	for idx, row := range rows {
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatalf("marshal row %d: %v", idx, err)
		}
		if _, err := file.Write(append(raw, '\n')); err != nil {
			t.Fatalf("write row %d: %v", idx, err)
		}
	}
}

