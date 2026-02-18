package main

import (
	"os"
	"strings"
	"testing"
)

func TestAppendPaasAgentRunRecordWritesArtifact(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	_, artifactPath, err := appendPaasAgentRunRecord(paasAgentRunRecord{
		Agent:          "ops-agent",
		RunID:          "run-20260218T180000Z",
		Status:         "queued",
		IncidentID:     "inc-1",
		IncidentCorrID: "corr-1",
		ExecutionMode:  paasAgentExecutionModeOfflineFakeCodex,
		ExecutionNote:  "deterministic action",
		Message:        "queued remediation",
	})
	if err != nil {
		t.Fatalf("append run record: %v", err)
	}
	if strings.TrimSpace(artifactPath) == "" {
		t.Fatalf("expected artifact path")
	}
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("expected artifact file: %v", err)
	}

	artifact, err := loadPaasAgentRunArtifact(artifactPath)
	if err != nil {
		t.Fatalf("load artifact: %v", err)
	}
	if artifact.Context != currentPaasContext() || artifact.Source != "agent-run" {
		t.Fatalf("unexpected artifact metadata: %#v", artifact)
	}
	if artifact.Record.RunID != "run-20260218T180000Z" || artifact.Record.IncidentCorrID != "corr-1" {
		t.Fatalf("unexpected artifact record payload: %#v", artifact.Record)
	}

	rows, _, err := loadPaasAgentRunRecords("ops-agent", 1)
	if err != nil {
		t.Fatalf("load run records: %v", err)
	}
	if len(rows) != 1 || strings.TrimSpace(rows[0].ArtifactPath) == "" {
		t.Fatalf("expected run log row with artifact path, got %#v", rows)
	}
}
