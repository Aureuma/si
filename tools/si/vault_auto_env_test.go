package main

import (
	"os"
	"path/filepath"
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
		"build":  false,
	}
	for cmd, want := range cases {
		if got := shouldAutoHydrateVaultEnvForRootCommand(cmd); got != want {
			t.Fatalf("cmd=%q got=%t want=%t", cmd, got, want)
		}
	}
}

func TestHydrateProcessEnvFromVaultSetsMissingValues(t *testing.T) {
	workspace := t.TempDir()
	envDir := filepath.Join(workspace, "sampleapp")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatalf("mkdir env dir: %v", err)
	}
	envFile := filepath.Join(envDir, ".env.dev")
	keyringPath := filepath.Join(workspace, "si-vault-keyring.json")
	t.Setenv("SI_VAULT_ENV_FILE", envFile)
	t.Setenv("SI_VAULT_KEYRING_FILE", keyringPath)

	publicKey, privateKey, err := vault.GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair: %v", err)
	}
	cipher, err := vault.EncryptSIVaultValue("vault-secret-value", publicKey)
	if err != nil {
		t.Fatalf("EncryptSIVaultValue: %v", err)
	}

	settings := Settings{}
	applySettingsDefaults(&settings)
	settings.Vault.File = envFile
	target, err := resolveSIVaultTarget("", "", envFile)
	if err != nil {
		t.Fatalf("resolveSIVaultTarget: %v", err)
	}
	secretKey := "OPENAI_API_KEY"
	if err := os.WriteFile(envFile, []byte(secretKey+"="+cipher+"\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	keyring := siVaultKeyring{
		Entries: map[string]siVaultKeyMaterial{
			siVaultKeyringEntryKey(target.Repo, target.Env): {
				Repo:       target.Repo,
				Env:        target.Env,
				PublicKey:  publicKey,
				PrivateKey: privateKey,
			},
		},
	}
	if err := saveSIVaultKeyring(keyring); err != nil {
		t.Fatalf("saveSIVaultKeyring: %v", err)
	}

	_ = os.Unsetenv(secretKey)
	t.Cleanup(func() { _ = os.Unsetenv(secretKey) })

	setCount, err := hydrateProcessEnvFromVault(settings, "test_auto_env")
	if err != nil {
		t.Fatalf("hydrateProcessEnvFromVault: %v", err)
	}
	if setCount != 1 {
		t.Fatalf("setCount=%d want=1", setCount)
	}
	if got := os.Getenv(secretKey); got != "vault-secret-value" {
		t.Fatalf("secret mismatch: got %q", got)
	}
}

func TestHydrateProcessEnvFromVaultDoesNotOverrideExisting(t *testing.T) {
	workspace := t.TempDir()
	envDir := filepath.Join(workspace, "sampleapp")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatalf("mkdir env dir: %v", err)
	}
	envFile := filepath.Join(envDir, ".env.dev")
	keyringPath := filepath.Join(workspace, "si-vault-keyring.json")
	t.Setenv("SI_VAULT_ENV_FILE", envFile)
	t.Setenv("SI_VAULT_KEYRING_FILE", keyringPath)

	publicKey, privateKey, err := vault.GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair: %v", err)
	}
	cipher, err := vault.EncryptSIVaultValue("vault-overwrite-value", publicKey)
	if err != nil {
		t.Fatalf("EncryptSIVaultValue: %v", err)
	}

	settings := Settings{}
	applySettingsDefaults(&settings)
	settings.Vault.File = envFile
	target, err := resolveSIVaultTarget("", "", envFile)
	if err != nil {
		t.Fatalf("resolveSIVaultTarget: %v", err)
	}
	secretKey := "GITHUB_TOKEN"
	if err := os.WriteFile(envFile, []byte(secretKey+"="+cipher+"\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	keyring := siVaultKeyring{
		Entries: map[string]siVaultKeyMaterial{
			siVaultKeyringEntryKey(target.Repo, target.Env): {
				Repo:       target.Repo,
				Env:        target.Env,
				PublicKey:  publicKey,
				PrivateKey: privateKey,
			},
		},
	}
	if err := saveSIVaultKeyring(keyring); err != nil {
		t.Fatalf("saveSIVaultKeyring: %v", err)
	}

	t.Setenv(secretKey, "already-set")
	setCount, err := hydrateProcessEnvFromVault(settings, "test_auto_env_no_override")
	if err != nil {
		t.Fatalf("hydrateProcessEnvFromVault: %v", err)
	}
	if setCount != 0 {
		t.Fatalf("setCount=%d want=0", setCount)
	}
	if got := os.Getenv(secretKey); got != "already-set" {
		t.Fatalf("expected existing env preserved, got %q", got)
	}
}
