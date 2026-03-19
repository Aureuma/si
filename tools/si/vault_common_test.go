package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"si/tools/si/internal/vault"
)

func TestVaultRequireTrustedRequiresRustCLIByDefault(t *testing.T) {
	t.Setenv(siRustCLIBinEnv, filepath.Join(t.TempDir(), "missing-si-rs"))

	doc := vault.ParseDotenv([]byte("# si-vault:recipient age1example\nSECRET=value\n"))
	fp, err := vaultTrustFingerprint(doc)
	if err != nil {
		t.Fatalf("vaultTrustFingerprint: %v", err)
	}

	storePath := filepath.Join(t.TempDir(), "trust.json")
	store := &vault.TrustStore{SchemaVersion: 3}
	store.Upsert(vault.TrustEntry{
		RepoRoot:    "/repo",
		File:        "/repo/.env",
		Fingerprint: fp,
	})
	if err := store.Save(storePath); err != nil {
		t.Fatalf("save trust store: %v", err)
	}

	settings := Settings{}
	settings.Vault.TrustStore = storePath
	target := vault.Target{RepoRoot: "/repo", File: "/repo/.env"}

	if _, err := vaultRequireTrusted(settings, target, doc); err == nil {
		t.Fatalf("expected missing rust cli error")
	}
}

func TestVaultRequireTrustedDelegatesToRustLookupWhenConfigured(t *testing.T) {
	doc := vault.ParseDotenv([]byte("# si-vault:recipient age1example\nSECRET=value\n"))
	fp, err := vaultTrustFingerprint(doc)
	if err != nil {
		t.Fatalf("vaultTrustFingerprint: %v", err)
	}

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"found\":true,\"matches\":true,\"repo_root\":\"/repo\",\"file\":\"/repo/.env\",\"expected_fingerprint\":\"" + fp + "\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)

	settings := Settings{}
	settings.Vault.TrustStore = filepath.Join(dir, "trust.json")
	target := vault.Target{RepoRoot: "/repo", File: "/repo/.env"}

	got, err := vaultRequireTrusted(settings, target, doc)
	if err != nil {
		t.Fatalf("vaultRequireTrusted: %v", err)
	}
	if got != fp {
		t.Fatalf("expected fingerprint %q, got %q", fp, got)
	}

	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "vault\ntrust\nlookup") {
		t.Fatalf("expected Rust vault trust lookup invocation, got %q", string(argsData))
	}
}
