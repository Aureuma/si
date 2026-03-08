package main

import (
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

	env := map[string]string{
		"HOME":                      stateHome,
		"SI_SETTINGS_HOME":          stateHome,
		vault.SIVaultPublicKeyName:  publicKey,
		vault.SIVaultPrivateKeyName: privateKey,
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
