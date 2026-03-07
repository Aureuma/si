package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runHelper(args ...string) (int, string, string) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestWriteCreatesSettingsWhenMissing(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "one", "settings.toml")
	code, _, stderr := runHelper("--settings", settingsPath, "--default-browser", "safari")
	if code != 0 {
		t.Fatalf("run failed code=%d stderr=%q", code, stderr)
	}
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, "[codex.login]") {
		t.Fatalf("missing [codex.login]: %q", got)
	}
	if !strings.Contains(got, `default_browser = "safari"`) {
		t.Fatalf("missing safari browser value: %q", got)
	}
}

func TestWriteUpdatesExistingDefaultBrowser(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "two", "settings.toml")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	initial := "[codex.login]\nopen_url = true\ndefault_browser = \"chrome\"\n"
	if err := os.WriteFile(settingsPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write initial settings: %v", err)
	}

	code, _, stderr := runHelper("--settings", settingsPath, "--default-browser", "safari")
	if code != 0 {
		t.Fatalf("run failed code=%d stderr=%q", code, stderr)
	}
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read updated settings: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, "open_url = true") {
		t.Fatalf("expected open_url preserved: %q", got)
	}
	if strings.Count(got, "default_browser = ") != 1 {
		t.Fatalf("expected one default_browser line: %q", got)
	}
	if !strings.Contains(got, `default_browser = "safari"`) {
		t.Fatalf("expected browser updated to safari: %q", got)
	}
}

func TestWriteAppendsCodexLoginWhenAbsent(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "three", "settings.toml")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	initial := "[codex]\nimage = \"aureuma/si:local\"\n"
	if err := os.WriteFile(settingsPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write initial settings: %v", err)
	}

	code, _, stderr := runHelper("--settings", settingsPath, "--default-browser", "chrome")
	if code != 0 {
		t.Fatalf("run failed code=%d stderr=%q", code, stderr)
	}
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read updated settings: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, "[codex]\n") {
		t.Fatalf("expected [codex] preserved: %q", got)
	}
	if !strings.Contains(got, "[codex.login]\n") {
		t.Fatalf("expected [codex.login] appended: %q", got)
	}
	if !strings.Contains(got, `default_browser = "chrome"`) {
		t.Fatalf("expected default browser line: %q", got)
	}
}

func TestUnsupportedBrowserFails(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "bad", "settings.toml")
	code, _, stderr := runHelper("--settings", settingsPath, "--default-browser", "firefox")
	if code == 0 {
		t.Fatalf("expected failure for unsupported browser")
	}
	if !strings.Contains(stderr, "--default-browser must be safari or chrome") {
		t.Fatalf("unexpected error: %q", stderr)
	}
}

func TestPrintDoesNotMutateFile(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "four", "settings.toml")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	initial := "[codex.login]\ndefault_browser = \"chrome\"\n"
	if err := os.WriteFile(settingsPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write initial settings: %v", err)
	}

	code, stdout, stderr := runHelper("--settings", settingsPath, "--default-browser", "safari", "--print")
	if code != 0 {
		t.Fatalf("run failed code=%d stderr=%q", code, stderr)
	}
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if string(raw) != initial {
		t.Fatalf("expected settings file unchanged; got %q", string(raw))
	}
	if !strings.Contains(stdout, `default_browser = "safari"`) {
		t.Fatalf("expected printed output to contain safari value: %q", stdout)
	}
}

func TestCheckModePassesWhenNoChangeNeeded(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "five", "settings.toml")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	initial := "[codex.login]\ndefault_browser = \"chrome\"\n"
	if err := os.WriteFile(settingsPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write initial settings: %v", err)
	}

	code, _, stderr := runHelper("--settings", settingsPath, "--default-browser", "chrome", "--check")
	if code != 0 {
		t.Fatalf("expected check success; code=%d stderr=%q", code, stderr)
	}
}

func TestCheckModeFailsWhenChangeNeeded(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "six", "settings.toml")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	initial := "[codex.login]\ndefault_browser = \"chrome\"\n"
	if err := os.WriteFile(settingsPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write initial settings: %v", err)
	}

	code, _, _ := runHelper("--settings", settingsPath, "--default-browser", "safari", "--check")
	if code == 0 {
		t.Fatalf("expected check failure when settings would change")
	}
}

func TestCheckModeFailsWhenFileMissing(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "missing", "settings.toml")
	code, _, _ := runHelper("--settings", settingsPath, "--default-browser", "safari", "--check")
	if code == 0 {
		t.Fatalf("expected check failure for missing settings file")
	}
}
