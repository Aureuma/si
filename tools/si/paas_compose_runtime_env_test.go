package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"si/tools/si/internal/vault"
)

func TestMaterializePaasComposeRuntimeEnvWritesReferencedKeysOnly(t *testing.T) {
	bundleDir := t.TempDir()
	composeBody := strings.Join([]string{
		"services:",
		"  api:",
		"    image: nginx:latest",
		"    environment:",
		"      - API_TOKEN=${API_TOKEN}",
		"      - OPTIONAL_VALUE=${OPTIONAL_VALUE:-fallback}",
		"      - MISSING_VALUE=${MISSING_VALUE}",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(bundleDir, "compose.yaml"), []byte(composeBody), 0o600); err != nil {
		t.Fatalf("write compose: %v", err)
	}

	count, err := materializePaasComposeRuntimeEnv(bundleDir, []string{"compose.yaml"}, []string{
		"API_TOKEN=abc #123",
		"OPTIONAL_VALUE=set-value",
		"UNUSED_VALUE=ignored",
	})
	if err != nil {
		t.Fatalf("materialize runtime env: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 resolved runtime env keys, got %d", count)
	}

	doc, err := vault.ReadDotenvFile(filepath.Join(bundleDir, ".env"))
	if err != nil {
		t.Fatalf("read generated runtime env: %v", err)
	}
	rawToken, ok := doc.Lookup("API_TOKEN")
	if !ok {
		t.Fatalf("expected API_TOKEN in runtime env")
	}
	token, err := vault.NormalizeDotenvValue(rawToken)
	if err != nil {
		t.Fatalf("normalize API_TOKEN: %v", err)
	}
	if token != "abc #123" {
		t.Fatalf("unexpected API_TOKEN value: got %q", token)
	}
	if _, ok := doc.Lookup("OPTIONAL_VALUE"); !ok {
		t.Fatalf("expected OPTIONAL_VALUE in runtime env")
	}
	if _, ok := doc.Lookup("UNUSED_VALUE"); ok {
		t.Fatalf("did not expect UNUSED_VALUE in runtime env")
	}
	if _, ok := doc.Lookup("MISSING_VALUE"); ok {
		t.Fatalf("did not expect unresolved MISSING_VALUE in runtime env")
	}
}

func TestMaterializePaasComposeRuntimeEnvRemovesStaleFileWhenNoKeysResolved(t *testing.T) {
	bundleDir := t.TempDir()
	composeBody := strings.Join([]string{
		"services:",
		"  api:",
		"    image: nginx:latest",
		"    environment:",
		"      - REQUIRED_VALUE=${REQUIRED_VALUE}",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(bundleDir, "compose.yaml"), []byte(composeBody), 0o600); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, ".env"), []byte("STALE=1\n"), 0o600); err != nil {
		t.Fatalf("seed stale runtime env: %v", err)
	}

	count, err := materializePaasComposeRuntimeEnv(bundleDir, []string{"compose.yaml"}, []string{"OTHER=1"})
	if err != nil {
		t.Fatalf("materialize runtime env: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected zero resolved keys, got %d", count)
	}
	if _, err := os.Stat(filepath.Join(bundleDir, ".env")); !os.IsNotExist(err) {
		t.Fatalf("expected runtime env file removed when unresolved; stat err=%v", err)
	}
}

func TestMaterializePaasComposeRuntimeEnvProjectsNamespacedSecrets(t *testing.T) {
	bundleDir := t.TempDir()
	composeBody := strings.Join([]string{
		"services:",
		"  auth:",
		"    environment:",
		"      - RM_GOTRUE_EXTERNAL_GOOGLE_ENABLED=${RM_GOTRUE_EXTERNAL_GOOGLE_ENABLED}",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(bundleDir, "compose.yaml"), []byte(composeBody), 0o600); err != nil {
		t.Fatalf("write compose: %v", err)
	}

	count, err := materializePaasComposeRuntimeEnv(bundleDir, []string{"compose.yaml"}, []string{
		"PAAS__CTX_DEFAULT__NS_DEFAULT__APP_RM__TARGET_VANGUARDA__VAR_RM_GOTRUE_EXTERNAL_GOOGLE_ENABLED=true",
	})
	if err != nil {
		t.Fatalf("materialize runtime env: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 resolved key, got %d", count)
	}

	doc, err := vault.ReadDotenvFile(filepath.Join(bundleDir, ".env"))
	if err != nil {
		t.Fatalf("read runtime env: %v", err)
	}
	raw, ok := doc.Lookup("RM_GOTRUE_EXTERNAL_GOOGLE_ENABLED")
	if !ok {
		t.Fatalf("expected projected RM_GOTRUE_EXTERNAL_GOOGLE_ENABLED key")
	}
	value, err := vault.NormalizeDotenvValue(raw)
	if err != nil {
		t.Fatalf("normalize projected key: %v", err)
	}
	if value != "true" {
		t.Fatalf("unexpected projected value: got %q", value)
	}
}
