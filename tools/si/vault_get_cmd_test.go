package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"si/tools/si/internal/vault"
)

func TestVaultGetAcceptsTrailingFlagsAfterKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess CLI test in short mode")
	}
	stateHome := t.TempDir()
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("TRAILING_GET_KEY=ok-value\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	publicKey, privateKey, err := vault.GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair: %v", err)
	}
	keyringPath := filepath.Join(stateHome, ".si", "vault", "si-vault-keyring.json")
	writeVaultTestKeyring(t, keyringPath, publicKey, privateKey)

	env := map[string]string{
		"HOME":                  stateHome,
		"SI_SETTINGS_HOME":      stateHome,
		"SI_VAULT_KEYRING_FILE": keyringPath,
	}
	scope := "trailing-get"
	stdout, stderr, err := runSICommand(t, env, "vault", "get", "TRAILING_GET_KEY", "--env-file", envFile, "--scope", scope, "--reveal")
	if err != nil {
		t.Fatalf("vault get failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "ok-value" {
		t.Fatalf("unexpected vault get output: %q", strings.TrimSpace(stdout))
	}
}

func writeVaultTestKeyring(t *testing.T, path, publicKey, privateKey string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir keyring dir: %v", err)
	}
	payload := map[string]any{
		"entries": map[string]any{
			"safe/dev": map[string]any{
				"repo":        "safe",
				"env":         "dev",
				"public_key":  publicKey,
				"private_key": privateKey,
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal keyring: %v", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatalf("write keyring: %v", err)
	}
}
