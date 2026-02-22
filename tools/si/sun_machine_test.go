package main

import (
	"strings"
	"testing"
)

func TestNormalizeMachineOperatorIDs(t *testing.T) {
	got := normalizeMachineOperatorIDs([]string{"op:bob@mbp", " op:bob@mbp ", "op:alice@ci", "", "op:alice@ci"})
	if len(got) != 2 {
		t.Fatalf("expected 2 unique operators, got %d: %#v", len(got), got)
	}
	if got[0] != "op:alice@ci" || got[1] != "op:bob@mbp" {
		t.Fatalf("unexpected normalized operators: %#v", got)
	}
}

func TestHeliaMachineOperatorAllowed(t *testing.T) {
	record := heliaMachineRecord{
		OwnerOperator: "op:owner@host",
		ACL: heliaMachineAccessControl{
			AllowedOperators: []string{"op:dev@host"},
		},
	}
	if !heliaMachineOperatorAllowed(record, "op:owner@host") {
		t.Fatalf("owner should be allowed")
	}
	if !heliaMachineOperatorAllowed(record, "op:dev@host") {
		t.Fatalf("allowlisted operator should be allowed")
	}
	if heliaMachineOperatorAllowed(record, "op:other@host") {
		t.Fatalf("unexpected allow for non-listed operator")
	}
}

func TestNormalizeMachineJobStatus(t *testing.T) {
	cases := map[string]string{
		"":          heliaMachineJobStatusQueued,
		"pending":   heliaMachineJobStatusQueued,
		"running":   heliaMachineJobStatusRunning,
		"succeeded": heliaMachineJobStatusSucceeded,
		"error":     heliaMachineJobStatusFailed,
		"forbidden": heliaMachineJobStatusDenied,
		"unknown":   "",
	}
	for in, want := range cases {
		if got := normalizeMachineJobStatus(in); got != want {
			t.Fatalf("normalizeMachineJobStatus(%q)=%q want %q", in, got, want)
		}
	}
}

func TestHeliaMachineJobFailureError(t *testing.T) {
	if err := heliaMachineJobFailureError(heliaMachineJob{JobID: "job-ok", Status: heliaMachineJobStatusSucceeded}); err != nil {
		t.Fatalf("expected succeeded job to return nil error, got: %v", err)
	}

	failed := heliaMachineJob{
		JobID:    "job-failed",
		Status:   heliaMachineJobStatusFailed,
		ExitCode: 2,
		Error:    "command exited with code 2",
	}
	err := heliaMachineJobFailureError(failed)
	if err == nil {
		t.Fatalf("expected failed job to return error")
	}
	if !strings.Contains(err.Error(), "job-failed") || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("unexpected failed error text: %v", err)
	}

	denied := heliaMachineJob{
		JobID:    "job-denied",
		Status:   heliaMachineJobStatusDenied,
		ExitCode: 1,
	}
	err = heliaMachineJobFailureError(denied)
	if err == nil {
		t.Fatalf("expected denied job to return error")
	}
	if !strings.Contains(err.Error(), "exit code 1") {
		t.Fatalf("expected denied error to include exit code, got: %v", err)
	}
}
