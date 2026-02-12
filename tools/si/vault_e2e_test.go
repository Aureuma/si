package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"si/tools/si/internal/vault"
)

func TestVaultE2E_InitSupportsArbitraryDotenvPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	tempState := t.TempDir()
	targetRoot := t.TempDir()
	envFile := filepath.Join(targetRoot, "plain-vault", ".env.app")
	keyFile := filepath.Join(tempState, "vault", "keys", "age.key")
	trustFile := filepath.Join(tempState, "vault", "trust.json")
	auditLog := filepath.Join(tempState, "vault", "audit.log")

	env := map[string]string{
		"HOME":                 tempState,
		"GOFLAGS":              "-modcacherw",
		"GOMODCACHE":           filepath.Join(tempState, "go-mod-cache"),
		"GOCACHE":              filepath.Join(tempState, "go-build-cache"),
		"SI_VAULT_KEY_BACKEND": "file",
		"SI_VAULT_KEY_FILE":    keyFile,
		"SI_VAULT_TRUST_STORE": trustFile,
		"SI_VAULT_AUDIT_LOG":   auditLog,
	}
	stdout, stderr, err := runSICommand(t, env,
		"vault", "init",
		"--file", envFile,
	)
	if err != nil {
		t.Fatalf("vault init failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	raw, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("expected env file to be created: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, vault.VaultHeaderVersionLine) {
		t.Fatalf("expected vault header, got:\n%s", content)
	}
	if !strings.Contains(content, "# si-vault:recipient age1") {
		t.Fatalf("expected recipient header, got:\n%s", content)
	}
	if !strings.Contains(stdout, filepath.Clean(envFile)) {
		t.Fatalf("expected init output to mention env file path, got:\n%s", stdout)
	}

	stdout, stderr, err = runSICommand(t, env, "vault", "trust", "status", "--file", envFile)
	if err != nil {
		t.Fatalf("vault trust status failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "trust:      ok") {
		t.Fatalf("expected trust status to be ok, got:\n%s", stdout)
	}
}
