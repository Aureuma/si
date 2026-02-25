package main

import (
	"os"
	"strings"
	"testing"

	"si/tools/si/internal/vault"
)

func TestVaultAutoHydrationEnabled(t *testing.T) {
	t.Setenv(siVaultAutoEnvKey, "")
	if !vaultAutoHydrationEnabled() {
		t.Fatalf("expected default auto-hydration enabled")
	}

	t.Setenv(siVaultAutoEnvKey, "false")
	if vaultAutoHydrationEnabled() {
		t.Fatalf("expected auto-hydration disabled for false")
	}

	t.Setenv(siVaultAutoEnvKey, "1")
	if !vaultAutoHydrationEnabled() {
		t.Fatalf("expected auto-hydration enabled for 1")
	}
}

func TestShouldAutoHydrateVaultEnvForRootCommand(t *testing.T) {
	cases := map[string]bool{
		"github": true,
		"cf":     true,
		"gcp":    true,
		"vault":  false,
		"sun":    false,
		"build":  false,
	}
	for cmd, want := range cases {
		if got := shouldAutoHydrateVaultEnvForRootCommand(cmd); got != want {
			t.Fatalf("cmd=%q got=%t want=%t", cmd, got, want)
		}
	}
}

func TestHydrateProcessEnvFromSunVaultSetsMissingValues(t *testing.T) {
	server, store := newSunTestServer(t, "acme", "token-vault-auto-env")
	defer server.Close()

	identity, err := vault.GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	cipher, err := vault.EncryptStringV1("vault-secret-value", []string{identity.Recipient().String()})
	if err != nil {
		t.Fatalf("EncryptStringV1: %v", err)
	}

	settings := Settings{}
	applySettingsDefaults(&settings)
	settings.Sun.BaseURL = server.URL
	settings.Sun.Token = "token-vault-auto-env"
	settings.Sun.Account = "acme"
	settings.Vault.File = "auto-env-scope"

	target, err := vaultResolveTarget(settings, "", true)
	if err != nil {
		t.Fatalf("vaultResolveTarget: %v", err)
	}
	kind := vaultSunKVKind(target)
	store.mu.Lock()
	identityKey := store.key(sunVaultIdentityKind, "default")
	store.payloads[identityKey] = []byte(strings.TrimSpace(identity.String()) + "\n")
	store.revs[identityKey] = 1
	store.metadata[identityKey] = map[string]any{}
	store.created[identityKey] = "2026-01-01T00:00:00Z"
	store.updated[identityKey] = "2026-01-02T00:00:00Z"

	secretKey := "OPENAI_API_KEY"
	secretObject := store.key(kind, secretKey)
	store.payloads[secretObject] = []byte(strings.TrimSpace(cipher) + "\n")
	store.revs[secretObject] = 1
	store.metadata[secretObject] = map[string]any{"deleted": false}
	store.created[secretObject] = "2026-01-01T00:00:00Z"
	store.updated[secretObject] = "2026-01-02T00:00:00Z"
	store.mu.Unlock()

	_ = os.Unsetenv(secretKey)
	t.Cleanup(func() { _ = os.Unsetenv(secretKey) })

	setCount, err := hydrateProcessEnvFromSunVault(settings, "test_auto_env")
	if err != nil {
		t.Fatalf("hydrateProcessEnvFromSunVault: %v", err)
	}
	if setCount != 1 {
		t.Fatalf("setCount=%d want=1", setCount)
	}
	if got := os.Getenv(secretKey); got != "vault-secret-value" {
		t.Fatalf("secret mismatch: got %q", got)
	}
}

func TestHydrateProcessEnvFromSunVaultDoesNotOverrideExisting(t *testing.T) {
	server, store := newSunTestServer(t, "acme", "token-vault-auto-env-no-override")
	defer server.Close()

	identity, err := vault.GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	cipher, err := vault.EncryptStringV1("vault-overwrite-value", []string{identity.Recipient().String()})
	if err != nil {
		t.Fatalf("EncryptStringV1: %v", err)
	}

	settings := Settings{}
	applySettingsDefaults(&settings)
	settings.Sun.BaseURL = server.URL
	settings.Sun.Token = "token-vault-auto-env-no-override"
	settings.Sun.Account = "acme"
	settings.Vault.File = "auto-env-scope-no-override"

	target, err := vaultResolveTarget(settings, "", true)
	if err != nil {
		t.Fatalf("vaultResolveTarget: %v", err)
	}
	kind := vaultSunKVKind(target)
	store.mu.Lock()
	identityKey := store.key(sunVaultIdentityKind, "default")
	store.payloads[identityKey] = []byte(strings.TrimSpace(identity.String()) + "\n")
	store.revs[identityKey] = 1
	store.metadata[identityKey] = map[string]any{}
	store.created[identityKey] = "2026-01-01T00:00:00Z"
	store.updated[identityKey] = "2026-01-02T00:00:00Z"

	secretKey := "GITHUB_TOKEN"
	secretObject := store.key(kind, secretKey)
	store.payloads[secretObject] = []byte(strings.TrimSpace(cipher) + "\n")
	store.revs[secretObject] = 1
	store.metadata[secretObject] = map[string]any{"deleted": false}
	store.created[secretObject] = "2026-01-01T00:00:00Z"
	store.updated[secretObject] = "2026-01-02T00:00:00Z"
	store.mu.Unlock()

	t.Setenv(secretKey, "already-set")
	setCount, err := hydrateProcessEnvFromSunVault(settings, "test_auto_env_no_override")
	if err != nil {
		t.Fatalf("hydrateProcessEnvFromSunVault: %v", err)
	}
	if setCount != 0 {
		t.Fatalf("setCount=%d want=0", setCount)
	}
	if got := os.Getenv(secretKey); got != "already-set" {
		t.Fatalf("expected existing env preserved, got %q", got)
	}
}
