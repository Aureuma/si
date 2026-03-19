package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowUsageUsesResetCountdown(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	resetAt := now.Add(130 * time.Minute).Unix()
	window := &appRateLimitWindow{
		UsedPercent: 40,
		ResetsAt:    &resetAt,
	}
	_, remaining := windowUsage(window, 300, now)
	if remaining != 130 {
		t.Fatalf("expected reset countdown 130m, got %d", remaining)
	}
}

func TestCmdCodexStatusDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"model\":\"gpt-5.2-codex\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siRustCLILegacyToggleEnv, "")

	output := captureOutputForTest(t, func() {
		cmdCodexStatus([]string{"ferma", "--json"})
	})
	if !strings.Contains(output, "\"model\":\"gpt-5.2-codex\"") {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nstatus-read\nferma\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}
