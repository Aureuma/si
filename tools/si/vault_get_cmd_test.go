package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestVaultGetAcceptsTrailingFlagsAfterKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess CLI test in short mode")
	}

	tempState := t.TempDir()
	envFile := filepath.Join(t.TempDir(), ".env.test")
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

	stdout, stderr, err := runSICommand(t, env, "vault", "init", "--file", envFile)
	if err != nil {
		t.Fatalf("vault init failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, env, "vault", "set", "TRAILING_GET_KEY", "ok-value", "--file", envFile)
	if err != nil {
		t.Fatalf("vault set failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "vault", "get", "TRAILING_GET_KEY", "--file", envFile, "--reveal")
	if err != nil {
		t.Fatalf("vault get failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "ok-value" {
		t.Fatalf("unexpected vault get output: %q", strings.TrimSpace(stdout))
	}
}
