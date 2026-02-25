package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSunE2E_VaultSyncPushPullRoundTripViaVaultCommand(t *testing.T) {
	t.Skip("vault sync push/pull removed in Sun remote vault mode")
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server, _ := newSunTestServer(t, "acme", "token-sync-cmd")
	defer server.Close()

	home, env := setupSunAuthState(t, server.URL, "acme", "token-sync-cmd")
	keyFile := filepath.Join(home, ".si", "vault", "keys", "age.key")
	trustFile := filepath.Join(home, ".si", "vault", "trust.json")
	auditLog := filepath.Join(home, ".si", "vault", "audit.log")
	env["SI_VAULT_KEY_BACKEND"] = "file"
	env["SI_VAULT_KEY_FILE"] = keyFile
	env["SI_VAULT_TRUST_STORE"] = trustFile
	env["SI_VAULT_AUDIT_LOG"] = auditLog

	vaultFile := filepath.Join(home, ".si", "vault", ".env")
	stdout, stderr, err := runSICommand(t, env, "vault", "init", "--file", vaultFile, "--set-default")
	if err != nil {
		t.Fatalf("vault init failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, env, "vault", "set", "SUN_SYNC_CMD_TEST", "secret-value", "--file", vaultFile)
	if err != nil {
		t.Fatalf("vault set failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	before, err := os.ReadFile(vaultFile)
	if err != nil {
		t.Fatalf("read vault before backup: %v", err)
	}

	stdout, stderr, err = runSICommand(t, env, "vault", "sync", "push", "--file", vaultFile, "--name", "roundtrip-sync-cmd")
	if err != nil {
		t.Fatalf("vault sync push failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if err := os.Remove(vaultFile); err != nil {
		t.Fatalf("remove vault file: %v", err)
	}
	stdout, stderr, err = runSICommand(t, env, "vault", "sync", "pull", "--file", vaultFile, "--name", "roundtrip-sync-cmd")
	if err != nil {
		t.Fatalf("vault sync pull failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	after, err := os.ReadFile(vaultFile)
	if err != nil {
		t.Fatalf("read vault after restore: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("vault sync round-trip mismatch")
	}

	stdout, stderr, err = runSICommand(t, env, "vault", "sync", "status", "--file", vaultFile, "--name", "roundtrip-sync-cmd", "--json")
	if err != nil {
		t.Fatalf("vault sync status failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var status map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("parse vault sync status json: %v\nstdout=%s", err, stdout)
	}
	if got, ok := status["backup_exists"].(bool); !ok || !got {
		t.Fatalf("expected backup_exists=true, got: %v", status["backup_exists"])
	}
}

func TestSunE2E_VaultRecipientsMutationsAutoBackup(t *testing.T) {
	t.Skip("legacy recipients auto-backup behavior removed in Sun remote vault mode")
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server, store := newSunTestServer(t, "acme", "token-recipient-sync")
	defer server.Close()

	home, env := setupSunAuthState(t, server.URL, "acme", "token-recipient-sync")
	keyFile := filepath.Join(home, ".si", "vault", "keys", "age.key")
	trustFile := filepath.Join(home, ".si", "vault", "trust.json")
	auditLog := filepath.Join(home, ".si", "vault", "audit.log")
	env["SI_VAULT_KEY_BACKEND"] = "file"
	env["SI_VAULT_KEY_FILE"] = keyFile
	env["SI_VAULT_TRUST_STORE"] = trustFile
	env["SI_VAULT_AUDIT_LOG"] = auditLog
	env["SI_VAULT_SYNC_BACKEND"] = vaultSyncBackendSun

	vaultFile := filepath.Join(home, ".si", "vault", ".env")
	stdout, stderr, err := runSICommand(t, env, "vault", "init", "--file", vaultFile, "--set-default")
	if err != nil {
		t.Fatalf("vault init failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if got := store.putCount(); got != 2 {
		t.Fatalf("expected init identity+backup upload count=2, got %d", got)
	}

	const recipient = "age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"
	stdout, stderr, err = runSICommand(t, env, "vault", "recipients", "add", "--file", vaultFile, recipient)
	if err != nil {
		t.Fatalf("vault recipients add failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if got := store.putCount(); got != 3 {
		t.Fatalf("expected recipients add backup upload count=3, got %d", got)
	}

	stdout, stderr, err = runSICommand(t, env, "vault", "recipients", "remove", "--file", vaultFile, recipient)
	if err != nil {
		t.Fatalf("vault recipients remove failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if got := store.putCount(); got != 4 {
		t.Fatalf("expected recipients remove backup upload count=4, got %d", got)
	}
}

func TestSunE2E_VaultSunBackendWorksWithoutLocalKeyMaterial(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server, _ := newSunTestServer(t, "acme", "token-vault-sun-backend")
	defer server.Close()

	_, envA := setupSunAuthState(t, server.URL, "acme", "token-vault-sun-backend")
	scope := "cross-machine"
	stdout, stderr, err := runSICommand(t, envA, "vault", "init", "--scope", scope, "--set-default")
	if err != nil {
		t.Fatalf("vault init (machine A) failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, envA, "vault", "set", "SUN_BACKEND_SECRET", "cross-machine-secret", "--scope", scope)
	if err != nil {
		t.Fatalf("vault set (machine A) failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	homeB := t.TempDir()
	envB := map[string]string{
		"HOME":             homeB,
		"SI_SETTINGS_HOME": homeB,
	}
	stdout, stderr, err = runSICommand(t, envB, "sun", "auth", "login", "--url", server.URL, "--token", "token-vault-sun-backend", "--account", "acme")
	if err != nil {
		t.Fatalf("sun auth login (machine B) failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, envB, "vault", "backend", "status", "--json")
	if err != nil {
		t.Fatalf("vault backend status (machine B) failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var backendStatus map[string]any
	if err := json.Unmarshal([]byte(stdout), &backendStatus); err != nil {
		t.Fatalf("parse backend status json: %v\nstdout=%s", err, stdout)
	}
	if got, _ := backendStatus["mode"].(string); got != vaultSyncBackendSun {
		t.Fatalf("expected machine B backend mode=%s, got %q", vaultSyncBackendSun, got)
	}

	stdout, stderr, err = runSICommand(t, envB, "vault", "get", "--scope", scope, "--reveal", "SUN_BACKEND_SECRET")
	if err != nil {
		t.Fatalf("vault get --reveal (machine B) failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "cross-machine-secret" {
		t.Fatalf("unexpected revealed value on machine B: %q", strings.TrimSpace(stdout))
	}

	if _, err := os.Stat(filepath.Join(homeB, ".si", "vault", "keys", "age.key")); err == nil {
		t.Fatalf("did not expect local vault key file on machine B")
	}
}
