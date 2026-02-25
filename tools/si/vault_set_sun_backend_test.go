package main

import (
	"strings"
	"testing"

	"si/tools/si/internal/vault"
)

func TestVaultSetSunBackendEncryptsWithSunIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess CLI smoke in short mode")
	}

	server, store := newSunTestServer(t, "acme", "token-vault-set-sun")
	defer server.Close()

	_, env := setupSunAuthState(t, server.URL, "acme", "token-vault-set-sun")
	env["SI_VAULT_IDENTITY"] = ""
	env["SI_VAULT_PRIVATE_KEY"] = ""
	env["SI_VAULT_IDENTITY_FILE"] = ""

	legacyIdentity, err := vault.GenerateIdentity()
	if err != nil {
		t.Fatalf("generate legacy identity: %v", err)
	}
	sunIdentity, err := vault.GenerateIdentity()
	if err != nil {
		t.Fatalf("generate sun identity: %v", err)
	}

	store.mu.Lock()
	storeKey := store.key(sunVaultIdentityKind, "default")
	store.payloads[storeKey] = []byte(strings.TrimSpace(sunIdentity.String()) + "\n")
	store.revs[storeKey] = 1
	store.created[storeKey] = "2026-01-01T00:00:00Z"
	store.updated[storeKey] = "2026-01-02T00:00:00Z"
	store.mu.Unlock()

	scope := "sun-backend-test"
	stdout, stderr, err := runSICommand(t, env, "vault", "init", "--scope", scope, "--set-default")
	if err != nil {
		t.Fatalf("vault init failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "vault", "set", "SUN_ONLY_NEW", "fresh-secret", "--scope", scope)
	if err != nil {
		t.Fatalf("vault set failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	settings := Settings{}
	applySettingsDefaults(&settings)
	settings.Sun.BaseURL = server.URL
	settings.Sun.Token = "token-vault-set-sun"
	settings.Sun.Account = "acme"
	settings.Vault.File = scope
	target, err := vaultResolveTarget(settings, scope, true)
	if err != nil {
		t.Fatalf("vaultResolveTarget: %v", err)
	}
	payload, ok := store.get(vaultSunKVKind(target), "SUN_ONLY_NEW")
	if !ok || len(payload) == 0 {
		t.Fatalf("expected SUN_ONLY_NEW key in Sun KV")
	}
	cipher := strings.TrimSpace(string(payload))
	if !vault.IsEncryptedValueV1(cipher) {
		t.Fatalf("expected encrypted value for SUN_ONLY_NEW, got: %q", cipher)
	}

	plain, err := vault.DecryptStringV1(cipher, sunIdentity)
	if err != nil {
		t.Fatalf("decrypt with sun identity: %v", err)
	}
	if plain != "fresh-secret" {
		t.Fatalf("decrypted value mismatch: got %q want %q", plain, "fresh-secret")
	}
	if _, err := vault.DecryptStringV1(cipher, legacyIdentity); err == nil {
		t.Fatalf("expected decryption with legacy identity to fail for new key")
	}
}
