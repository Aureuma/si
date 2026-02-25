package main

import (
	"strings"
	"testing"

	"si/tools/si/internal/vault"
)

func sunTargetForScope(t *testing.T, serverURL string, token string, account string, scope string) vault.Target {
	t.Helper()
	settings := Settings{}
	applySettingsDefaults(&settings)
	settings.Sun.BaseURL = serverURL
	settings.Sun.Token = token
	settings.Sun.Account = account
	settings.Vault.File = scope
	target, err := vaultResolveTarget(settings, scope, true)
	if err != nil {
		t.Fatalf("vaultResolveTarget(%q): %v", scope, err)
	}
	return target
}

func seedSunPlaintextKey(store *sunObjectStore, kind string, key string, value string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	objectKey := store.key(kind, key)
	store.payloads[objectKey] = []byte(strings.TrimSpace(value) + "\n")
	store.revs[objectKey] = store.revs[objectKey] + 1
	if store.revs[objectKey] <= 0 {
		store.revs[objectKey] = 1
	}
	store.created[objectKey] = "2026-01-01T00:00:00Z"
	store.updated[objectKey] = "2026-01-02T00:00:00Z"
	store.metadata[objectKey] = map[string]any{
		"operation": "set",
		"deleted":   false,
	}
}

func TestVaultRecipientsListUsesSunIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess CLI test in short mode")
	}

	server, _ := newSunTestServer(t, "acme", "token-vault-recips-list")
	defer server.Close()

	_, env := setupSunAuthState(t, server.URL, "acme", "token-vault-recips-list")
	scope := "recipients-scope"
	if stdout, stderr, err := runSICommand(t, env, "vault", "init", "--scope", scope); err != nil {
		t.Fatalf("vault init failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err := runSICommand(t, env, "vault", "recipients", "list", "--scope", scope)
	if err != nil {
		t.Fatalf("vault recipients list failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "source: sun-identity") {
		t.Fatalf("expected sun identity source in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "scope: "+scope) {
		t.Fatalf("expected scope in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "age1") {
		t.Fatalf("expected recipient in output, got: %q", stdout)
	}
}

func TestVaultRecipientsAddUnsupportedInSunMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess CLI test in short mode")
	}

	server, _ := newSunTestServer(t, "acme", "token-vault-recips-add")
	defer server.Close()

	_, env := setupSunAuthState(t, server.URL, "acme", "token-vault-recips-add")
	stdout, stderr, err := runSICommand(t, env, "vault", "recipients", "add", "--scope", "recipients-scope", "age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq")
	if err == nil {
		t.Fatalf("expected recipients add to fail in Sun mode\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "not supported in Sun remote vault mode") {
		t.Fatalf("expected unsupported message, got stderr=%q stdout=%q", stderr, stdout)
	}
}

func TestVaultDecryptInPlaceUnsupportedInSunMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess CLI test in short mode")
	}

	server, _ := newSunTestServer(t, "acme", "token-vault-decrypt-inplace")
	defer server.Close()

	_, env := setupSunAuthState(t, server.URL, "acme", "token-vault-decrypt-inplace")
	stdout, stderr, err := runSICommand(t, env, "vault", "decrypt", "--scope", "default", "--in-place")
	if err == nil {
		t.Fatalf("expected vault decrypt --in-place to fail in Sun mode\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "not supported in Sun remote vault mode") {
		t.Fatalf("expected unsupported message, got stderr=%q stdout=%q", stderr, stdout)
	}
}

func TestVaultCheckSunModeDetectsPlaintextCloudKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess CLI test in short mode")
	}

	server, store := newSunTestServer(t, "acme", "token-vault-check-cloud")
	defer server.Close()

	_, env := setupSunAuthState(t, server.URL, "acme", "token-vault-check-cloud")
	scope := "check-cloud"
	if stdout, stderr, err := runSICommand(t, env, "vault", "init", "--scope", scope); err != nil {
		t.Fatalf("vault init failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	target := sunTargetForScope(t, server.URL, "token-vault-check-cloud", "acme", scope)
	seedSunPlaintextKey(store, vaultSunKVKind(target), "PLAINTEXT_CANARY", "plain-value")

	stdout, stderr, err := runSICommand(t, env, "vault", "check", "--scope", scope)
	if err == nil {
		t.Fatalf("expected vault check to fail when plaintext cloud key exists\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "PLAINTEXT_CANARY") {
		t.Fatalf("expected plaintext key in check output, got stderr=%q stdout=%q", stderr, stdout)
	}
	if !strings.Contains(stderr, "si vault encrypt --scope") {
		t.Fatalf("expected remediation command in output, got stderr=%q stdout=%q", stderr, stdout)
	}
}

func TestVaultEncryptSunModeEncryptsCloudPlaintextKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess CLI test in short mode")
	}

	server, store := newSunTestServer(t, "acme", "token-vault-encrypt-cloud")
	defer server.Close()

	_, env := setupSunAuthState(t, server.URL, "acme", "token-vault-encrypt-cloud")
	scope := "encrypt-cloud"
	if stdout, stderr, err := runSICommand(t, env, "vault", "init", "--scope", scope); err != nil {
		t.Fatalf("vault init failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	target := sunTargetForScope(t, server.URL, "token-vault-encrypt-cloud", "acme", scope)
	seedSunPlaintextKey(store, vaultSunKVKind(target), "NEEDS_ENCRYPTION", "plain-value")

	if stdout, stderr, err := runSICommand(t, env, "vault", "encrypt", "--scope", scope); err != nil {
		t.Fatalf("vault encrypt failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	raw, ok := store.get(vaultSunKVKind(target), "NEEDS_ENCRYPTION")
	if !ok {
		t.Fatalf("expected NEEDS_ENCRYPTION key in Sun store")
	}
	cipher := strings.TrimSpace(string(raw))
	if !vault.IsEncryptedValueV1(cipher) {
		t.Fatalf("expected encrypted payload after vault encrypt, got %q", cipher)
	}

	stdout, stderr, err := runSICommand(t, env, "vault", "get", "NEEDS_ENCRYPTION", "--scope", scope, "--reveal")
	if err != nil {
		t.Fatalf("vault get --reveal failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "plain-value" {
		t.Fatalf("unexpected revealed value: got %q want %q", strings.TrimSpace(stdout), "plain-value")
	}
}
