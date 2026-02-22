package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeVaultSyncBackend(t *testing.T) {
	cases := map[string]string{
		"git":   vaultSyncBackendGit,
		"local": vaultSyncBackendGit,
		"helia": vaultSyncBackendHelia,
		"cloud": vaultSyncBackendHelia,
		"dual":  vaultSyncBackendDual,
		"both":  vaultSyncBackendDual,
	}
	for in, want := range cases {
		if got := normalizeVaultSyncBackend(in); got != want {
			t.Fatalf("normalizeVaultSyncBackend(%q)=%q want=%q", in, got, want)
		}
	}
	if got := normalizeVaultSyncBackend("invalid"); got != "" {
		t.Fatalf("expected invalid backend to normalize to empty, got %q", got)
	}
}

func TestVaultBackendStatusAndUse(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess test in short mode")
	}
	home := t.TempDir()
	env := map[string]string{
		"HOME":             home,
		"SI_SETTINGS_HOME": home,
	}

	stdout, stderr, err := runSICommand(t, env, "vault", "backend", "status", "--json")
	if err != nil {
		t.Fatalf("backend status failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var before map[string]any
	if err := json.Unmarshal([]byte(stdout), &before); err != nil {
		t.Fatalf("parse backend status json: %v\nstdout=%s", err, stdout)
	}
	if before["mode"] != vaultSyncBackendGit {
		t.Fatalf("default mode=%v want=%s", before["mode"], vaultSyncBackendGit)
	}

	stdout, stderr, err = runSICommand(t, env, "vault", "backend", "use", "--mode", "dual")
	if err != nil {
		t.Fatalf("backend use failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "vault sync backend set to dual") {
		t.Fatalf("unexpected backend use output: %s", stdout)
	}

	stdout, stderr, err = runSICommand(t, env, "vault", "backend", "status", "--json")
	if err != nil {
		t.Fatalf("backend status after use failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var after map[string]any
	if err := json.Unmarshal([]byte(stdout), &after); err != nil {
		t.Fatalf("parse backend status json after use: %v\nstdout=%s", err, stdout)
	}
	if after["mode"] != vaultSyncBackendDual {
		t.Fatalf("mode after use=%v want=%s", after["mode"], vaultSyncBackendDual)
	}
	if after["source"] != "settings" {
		t.Fatalf("source after use=%v want=settings", after["source"])
	}
}

func TestVaultBackendStatusInvalidEnvOverrideFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess test in short mode")
	}
	home := t.TempDir()
	env := map[string]string{
		"HOME":                  home,
		"SI_SETTINGS_HOME":      home,
		"SI_VAULT_SYNC_BACKEND": "invalid",
	}
	stdout, stderr, err := runSICommand(t, env, "vault", "backend", "status", "--json")
	if err == nil {
		t.Fatalf("expected status with invalid env override to fail\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	combined := strings.ToLower(strings.TrimSpace(stdout + "\n" + stderr))
	if !strings.Contains(combined, "invalid si_vault_sync_backend") {
		t.Fatalf("expected invalid backend error, got stdout=%s stderr=%s", stdout, stderr)
	}
}
