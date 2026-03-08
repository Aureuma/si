package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVaultGuardContainerLocalAccessBlocksSensitiveCommands(t *testing.T) {
	t.Setenv("SI_CODEX_PROFILE_ID", "alpha")
	if err := vaultGuardContainerLocalAccess("get"); err == nil {
		t.Fatalf("expected guard to block si vault get in runtime container context")
	}
}

func TestVaultGuardContainerLocalAccessAllowsNonSensitiveCommands(t *testing.T) {
	t.Setenv("SI_CODEX_PROFILE_ID", "alpha")
	if err := vaultGuardContainerLocalAccess("docker"); err != nil {
		t.Fatalf("expected non-sensitive vault command to be allowed, got %v", err)
	}
}

func TestVaultListBlockedInContainerContext(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess CLI test in short mode")
	}
	home := t.TempDir()
	envFile := filepath.Join(t.TempDir(), ".env.dev")
	if err := os.WriteFile(envFile, []byte("TEST_KEY=hello\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	stdout, stderr, err := runSICommand(t, map[string]string{
		"HOME":                home,
		"SI_SETTINGS_HOME":    home,
		"SI_CODEX_PROFILE_ID": "alpha",
	}, "vault", "list", "--env-file", envFile)
	if err == nil {
		t.Fatalf("expected vault list to be blocked in container context\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	combined := strings.ToLower(stdout + "\n" + stderr)
	if !strings.Contains(combined, "blocked inside si runtime containers") {
		t.Fatalf("expected container block message, got\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	if !strings.Contains(combined, "si fort") {
		t.Fatalf("expected fort guidance in error message, got\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}
