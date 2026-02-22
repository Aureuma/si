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

func TestSunMachineOperatorAllowed(t *testing.T) {
	record := sunMachineRecord{
		OwnerOperator: "op:owner@host",
		ACL: sunMachineAccessControl{
			AllowedOperators: []string{"op:dev@host"},
		},
	}
	if !sunMachineOperatorAllowed(record, "op:owner@host") {
		t.Fatalf("owner should be allowed")
	}
	if !sunMachineOperatorAllowed(record, "op:dev@host") {
		t.Fatalf("allowlisted operator should be allowed")
	}
	if sunMachineOperatorAllowed(record, "op:other@host") {
		t.Fatalf("unexpected allow for non-listed operator")
	}
}

func TestNormalizeMachineJobStatus(t *testing.T) {
	cases := map[string]string{
		"":          sunMachineJobStatusQueued,
		"pending":   sunMachineJobStatusQueued,
		"running":   sunMachineJobStatusRunning,
		"succeeded": sunMachineJobStatusSucceeded,
		"error":     sunMachineJobStatusFailed,
		"forbidden": sunMachineJobStatusDenied,
		"unknown":   "",
	}
	for in, want := range cases {
		if got := normalizeMachineJobStatus(in); got != want {
			t.Fatalf("normalizeMachineJobStatus(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSunMachineJobFailureError(t *testing.T) {
	if err := sunMachineJobFailureError(sunMachineJob{JobID: "job-ok", Status: sunMachineJobStatusSucceeded}); err != nil {
		t.Fatalf("expected succeeded job to return nil error, got: %v", err)
	}

	failed := sunMachineJob{
		JobID:    "job-failed",
		Status:   sunMachineJobStatusFailed,
		ExitCode: 2,
		Error:    "command exited with code 2",
	}
	err := sunMachineJobFailureError(failed)
	if err == nil {
		t.Fatalf("expected failed job to return error")
	}
	if !strings.Contains(err.Error(), "job-failed") || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("unexpected failed error text: %v", err)
	}

	denied := sunMachineJob{
		JobID:    "job-denied",
		Status:   sunMachineJobStatusDenied,
		ExitCode: 1,
	}
	err = sunMachineJobFailureError(denied)
	if err == nil {
		t.Fatalf("expected denied job to return error")
	}
	if !strings.Contains(err.Error(), "exit code 1") {
		t.Fatalf("expected denied error to include exit code, got: %v", err)
	}
}
