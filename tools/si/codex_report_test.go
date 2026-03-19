package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractReportLinesSingle(t *testing.T) {
	raw := "› Hello\n\n• Hi there!\n\n› Next"
	segments := parsePromptSegments(raw)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	report := extractReportLines(segments[0].Lines)
	if report != "• Hi there!" {
		t.Fatalf("unexpected report: %q", report)
	}
}

func TestExtractReportLinesMultiline(t *testing.T) {
	raw := "› Prompt\n\n• The ocean covers most of Earth and helps regulate the climate. Its depths hold\n  vast ecosystems that are still largely unexplored.\n\n› Next"
	segments := parsePromptSegments(raw)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	report := extractReportLines(segments[0].Lines)
	expected := "• The ocean covers most of Earth and helps regulate the climate. Its depths hold\n  vast ecosystems that are still largely unexplored."
	if report != expected {
		t.Fatalf("unexpected report: %q", report)
	}
}

func TestExtractReportLinesStopsOnWarning(t *testing.T) {
	raw := "› Prompt\n\n• Hello!\n\n⚠ Heads up, you have less than 5% left.\n\n› Next"
	segments := parsePromptSegments(raw)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	report := extractReportLines(segments[0].Lines)
	if report != "• Hello!" {
		t.Fatalf("unexpected report: %q", report)
	}
}

func TestStripANSI(t *testing.T) {
	raw := "\x1b[32m• Hello\x1b[0m"
	clean := stripANSI(raw)
	if clean != "• Hello" {
		t.Fatalf("unexpected stripped value: %q", clean)
	}
}

func TestParsePromptSegmentsWithANSI(t *testing.T) {
	raw := "› Prompt\n\x1b[32m• Hello\x1b[0m\n\n› Next"
	segments := parsePromptSegments(stripANSI(raw))
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	report := extractReportLines(segments[0].Lines)
	if report != "• Hello" {
		t.Fatalf("unexpected report: %q", report)
	}
}

func TestExtractReportSkipsWorking(t *testing.T) {
	raw := "› Prompt\n\n• Working (0s • esc to interrupt)\n\n• Hello!\n\n› Next"
	segments := parsePromptSegments(raw)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	report := extractReportLines(segments[0].Lines)
	if report != "• Hello!" {
		t.Fatalf("unexpected report: %q", report)
	}
}

func TestExtractReportIncludesWorkedLine(t *testing.T) {
	raw := "› Prompt\n\n• Done.\n\nWorked for 0m 2s\n\n› Next"
	segments := parsePromptSegments(raw)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	report := extractReportLines(segments[0].Lines)
	expected := "• Done.\nWorked for 0m 2s"
	if report != expected {
		t.Fatalf("unexpected report: %q", report)
	}
}

func TestReadCodexReportCaptureDelegatesToRustWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\ncat >/dev/null\nprintf '%s\\n' '{\"segments\":[{\"prompt\":\"Prompt\",\"lines\":[\"• Done.\",\"Worked for 0m 2s\"],\"raw\":[\"• Done.\",\"Worked for 0m 2s\"]},{\"prompt\":\"Next\",\"lines\":[],\"raw\":[]}],\"report\":\"• Done.\\nWorked for 0m 2s\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siRustCLILegacyToggleEnv, "")

	segments, report, err := readCodexReportCapture("› Prompt\n• Done.\nWorked for 0m 2s\n› Next", "› Prompt\n• Done.\nWorked for 0m 2s\n› Next", 0, false)
	if err != nil {
		t.Fatalf("readCodexReportCapture: %v", err)
	}
	if len(segments) != 2 || segments[0].Prompt != "Prompt" {
		t.Fatalf("unexpected segments: %#v", segments)
	}
	if report != "• Done.\nWorked for 0m 2s" {
		t.Fatalf("unexpected report: %q", report)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nreport-parse\n--format\njson" {
		t.Fatalf("unexpected delegated args %q", string(argsData))
	}
}

func TestCmdCodexReportUsesInjectedHappyPath(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\ncat >/dev/null\nprintf '%s\\n' '{\"segments\":[{\"prompt\":\"Prompt\",\"lines\":[\"• Done.\"],\"raw\":[\"• Done.\"]},{\"prompt\":\"Next\",\"lines\":[],\"raw\":[]}],\"report\":\"• Done.\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siRustCLILegacyToggleEnv, "")

	prevLock := acquireCodexReportLockFn
	prevLookup := lookupCodexReportContainerFn
	prevEnsure := ensureCodexReportTmuxAvailableFn
	prevCleanup := cleanupCodexReportTmuxSessionsFn
	prevFetch := fetchCodexReportsViaTmuxFn
	t.Cleanup(func() {
		acquireCodexReportLockFn = prevLock
		lookupCodexReportContainerFn = prevLookup
		ensureCodexReportTmuxAvailableFn = prevEnsure
		cleanupCodexReportTmuxSessionsFn = prevCleanup
		fetchCodexReportsViaTmuxFn = prevFetch
	})

	acquireCodexReportLockFn = func(string, string, time.Duration, time.Duration) (func(), error) {
		return func() {}, nil
	}
	lookupCodexReportContainerFn = func(context.Context, string) (string, string, error) {
		return "si-codex-ferma", "container-id", nil
	}
	ensureCodexReportTmuxAvailableFn = func() error { return nil }
	cleanupCodexReportTmuxSessionsFn = func(context.Context, string, time.Duration, statusOptions) {}
	fetchCodexReportsViaTmuxFn = func(ctx context.Context, containerID string, prompts []string, opts reportOptions) (string, []codexTurnReport, error) {
		segments, report, err := readCodexReportCapture("› Prompt\n• Done.\n› Next", "› Prompt\n• Done.\n› Next", 0, false)
		if err != nil {
			return "", nil, err
		}
		if containerID != "container-id" {
			t.Fatalf("unexpected container id %q", containerID)
		}
		if len(prompts) != 1 || prompts[0] != "Prompt" {
			t.Fatalf("unexpected prompts %#v", prompts)
		}
		if len(segments) == 0 {
			t.Fatalf("expected parsed segments")
		}
		return "raw-output", []codexTurnReport{{Prompt: prompts[0], Report: report}}, nil
	}

	output := captureOutputForTest(t, func() {
		cmdCodexReport([]string{"ferma", "--prompt", "Prompt"})
	})
	if !strings.Contains(output, "Turn 1: Prompt") {
		t.Fatalf("unexpected output: %q", output)
	}
	if !strings.Contains(output, "• Done.") {
		t.Fatalf("unexpected output: %q", output)
	}

	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nreport-parse\n--format\njson" {
		t.Fatalf("unexpected delegated args %q", string(argsData))
	}
}
