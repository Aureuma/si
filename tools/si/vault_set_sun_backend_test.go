package main

import (
	"fmt"
	"os"
	"path/filepath"
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

	home, env := setupSunAuthState(t, server.URL, "acme", "token-vault-set-sun")
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

	legacyRecipient := strings.TrimSpace(legacyIdentity.Recipient().String())
	legacyCipher, err := vault.EncryptStringV1("legacy-value", []string{legacyRecipient})
	if err != nil {
		t.Fatalf("encrypt legacy value: %v", err)
	}
	vaultFile := filepath.Join(home, ".si", "vault", ".env")
	if err := os.MkdirAll(filepath.Dir(vaultFile), 0o700); err != nil {
		t.Fatalf("mkdir vault dir: %v", err)
	}
	initial := fmt.Sprintf(
		"# si-vault:v2\n# si-vault:recipient %s\n\nLEGACY_ONLY=%s\n",
		legacyRecipient,
		legacyCipher,
	)
	if err := os.WriteFile(vaultFile, []byte(initial), 0o600); err != nil {
		t.Fatalf("write vault file: %v", err)
	}

	stdout, stderr, err := runSICommand(t, env, "vault", "trust", "accept", "--yes", "--file", vaultFile)
	if err != nil {
		t.Fatalf("vault trust accept failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "vault", "set", "SUN_ONLY_NEW", "fresh-secret", "--file", vaultFile)
	if err != nil {
		t.Fatalf("vault set failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	doc, err := vault.ReadDotenvFile(vaultFile)
	if err != nil {
		t.Fatalf("read vault file: %v", err)
	}
	cipher, ok := doc.Lookup("SUN_ONLY_NEW")
	if !ok {
		t.Fatalf("expected SUN_ONLY_NEW key in vault file")
	}
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
