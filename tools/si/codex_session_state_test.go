package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexResumeProfileKey(t *testing.T) {
	if got := codexResumeProfileKey("alpha", "si-codex-beta"); got != "alpha" {
		t.Fatalf("expected explicit profile key, got %q", got)
	}
	if got := codexResumeProfileKey("", "si-codex-beta"); got != "beta" {
		t.Fatalf("expected slug fallback, got %q", got)
	}
}

func TestParseCodexSessionMetaLine(t *testing.T) {
	line := `{"timestamp":"2026-02-17T17:00:00Z","type":"session_meta","payload":{"id":"sess_abc123","cwd":"/workspace","timestamp":"2026-02-17T17:01:00Z"}}`
	record, ok := parseCodexSessionMetaLine(line)
	if !ok {
		t.Fatalf("expected valid parse")
	}
	if record.SessionID != "sess_abc123" {
		t.Fatalf("unexpected session id %q", record.SessionID)
	}
	if record.Cwd != "/workspace" {
		t.Fatalf("unexpected cwd %q", record.Cwd)
	}
	if record.RecordedAt != "2026-02-17T17:01:00Z" {
		t.Fatalf("unexpected recorded timestamp %q", record.RecordedAt)
	}
}

func TestParseCodexSessionMetaLineTimestampFallback(t *testing.T) {
	line := `{"timestamp":"2026-02-17T17:00:00Z","type":"session_meta","payload":{"id":"sess_abc123","cwd":"/workspace"}}`
	record, ok := parseCodexSessionMetaLine(line)
	if !ok {
		t.Fatalf("expected valid parse")
	}
	if record.RecordedAt != "2026-02-17T17:00:00Z" {
		t.Fatalf("expected top-level timestamp fallback, got %q", record.RecordedAt)
	}
}

func TestParseCodexSessionMetaLineInvalid(t *testing.T) {
	if _, ok := parseCodexSessionMetaLine(""); ok {
		t.Fatalf("expected empty line to fail parse")
	}
	if _, ok := parseCodexSessionMetaLine(`{"type":"response_item","payload":{"id":"sess_abc123"}}`); ok {
		t.Fatalf("expected non-session_meta event to fail parse")
	}
	if _, ok := parseCodexSessionMetaLine(`{"type":"session_meta","payload":{"id":""}}`); ok {
		t.Fatalf("expected empty session id to fail parse")
	}
}

func TestCodexProfileSessionRecordRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	record := codexSessionRecord{
		SessionID: "sess_abc123",
		Cwd:       "/workspace",
	}
	if err := saveCodexProfileSessionRecord("alpha", record); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	loaded, err := loadCodexProfileSessionRecord("alpha")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.SessionID != "sess_abc123" {
		t.Fatalf("unexpected session id %q", loaded.SessionID)
	}
	if loaded.Cwd != "/workspace" {
		t.Fatalf("unexpected cwd %q", loaded.Cwd)
	}
	if strings.TrimSpace(loaded.RecordedAt) == "" {
		t.Fatalf("expected recorded timestamp to be populated")
	}
	path, err := codexProfileSessionRecordPath("alpha")
	if err != nil {
		t.Fatalf("path failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected record file to exist: %v", err)
	}
}

func TestSaveCodexProfileSessionRecordEmptySessionIsNoOp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := saveCodexProfileSessionRecord("alpha", codexSessionRecord{}); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	path := filepath.Join(home, ".si", "codex", "profiles", "alpha", codexProfileSessionRecordFilename)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no record file, stat err=%v", err)
	}
}

func TestCodexProfileSessionRecordPathRejectsInvalidProfile(t *testing.T) {
	if _, err := codexProfileSessionRecordPath("bad/path"); err == nil {
		t.Fatalf("expected invalid profile key error")
	}
}

func TestBuildCodexTmuxResumeCommand(t *testing.T) {
	cmd := buildCodexTmuxResumeCommand("si-codex-alpha", "/tmp/work", "sess_abc123", "alpha")
	if !strings.Contains(cmd, "codex resume") || !strings.Contains(cmd, "sess_abc123") || !strings.Contains(cmd, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("expected resume invocation in tmux command, got: %s", cmd)
	}
	if !strings.Contains(cmd, "tmux session unavailable; attempting codex resume") {
		t.Fatalf("expected announcement in tmux command, got: %s", cmd)
	}
	if !strings.Contains(cmd, "exec bash -il") {
		t.Fatalf("expected interactive shell handoff, got: %s", cmd)
	}
}

func TestBuildCodexTmuxResumeCommandEmptySession(t *testing.T) {
	if got := buildCodexTmuxResumeCommand("si-codex-alpha", "/tmp/work", "", "alpha"); got != "" {
		t.Fatalf("expected empty command when no session id, got %q", got)
	}
}

func TestCodexSelectTmuxLaunchCommand(t *testing.T) {
	if got, resumed := codexSelectTmuxLaunchCommand("normal", ""); got != "normal" || resumed {
		t.Fatalf("expected normal command without resume, got %q resumed=%v", got, resumed)
	}
	if got, resumed := codexSelectTmuxLaunchCommand("normal", "resume"); got != "resume" || !resumed {
		t.Fatalf("expected resume command to win, got %q resumed=%v", got, resumed)
	}
}
