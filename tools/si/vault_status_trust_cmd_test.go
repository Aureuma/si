package main

import (
	"strings"
	"testing"
)

func TestVaultTrustStatusMissingFileIsNonFatal(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess test in short mode")
	}
	home := t.TempDir()
	env := map[string]string{
		"HOME":             home,
		"SI_SETTINGS_HOME": home,
	}
	stdout, stderr, err := runSICommand(t, env, "vault", "trust", "status")
	if err != nil {
		t.Fatalf("vault trust status should not fail for missing file: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "env file:") {
		t.Fatalf("expected env file line, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "trust:      unavailable (env file missing)") {
		t.Fatalf("expected missing-file trust status, got:\n%s", stdout)
	}
}

func TestVaultStatusSunModeWithoutAuthFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess test in short mode")
	}
	home := t.TempDir()
	env := map[string]string{
		"HOME":                  home,
		"SI_SETTINGS_HOME":      home,
		"SI_VAULT_SYNC_BACKEND": "sun",
	}
	stdout, stderr, err := runSICommand(t, env, "vault", "status")
	if err == nil {
		t.Fatalf("vault status should fail when sun auth is missing\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	combined := strings.ToLower(strings.TrimSpace(stdout + "\n" + stderr))
	if !strings.Contains(combined, "sun token is required") {
		t.Fatalf("expected sun auth failure, got stdout=%s stderr=%s", stdout, stderr)
	}
}
