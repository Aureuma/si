package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVaultStatusFailsFastOnSettingsParseError(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess CLI test in short mode")
	}
	home := t.TempDir()
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("TEST_KEY=test\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	fortDir := filepath.Join(home, ".si", "fort")
	if err := os.MkdirAll(fortDir, 0o700); err != nil {
		t.Fatalf("mkdir fort settings dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fortDir, "settings.toml"), []byte("[fort\nhost='https://fort.example'\n"), 0o600); err != nil {
		t.Fatalf("write malformed settings: %v", err)
	}

	stdout, stderr, err := runSICommand(t, map[string]string{
		"HOME":                home,
		"SI_SETTINGS_HOME":    home,
		"SI_CODEX_PROFILE_ID": "",
		"CODEX_HOME":          "",
	}, "vault", "status", "--env-file", envFile, "--json")
	if err == nil {
		t.Fatalf("expected vault status to fail with malformed settings\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	combined := strings.ToLower(stdout + "\n" + stderr)
	if !strings.Contains(combined, "vault settings load failed") {
		t.Fatalf("expected strict vault settings error, got\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	if !strings.Contains(combined, "parse settings module fort") {
		t.Fatalf("expected parse module context in error, got\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}
