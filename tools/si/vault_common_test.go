package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"si/tools/si/internal/vault"
)

func TestVaultValidateImplicitTargetRepoScopeBlocksCrossRepoDefault(t *testing.T) {
	currentRepo := t.TempDir()
	otherRepo := t.TempDir()
	mustMkdir(t, filepath.Join(currentRepo, "configs"))
	mustMkdir(t, filepath.Join(currentRepo, "agents"))

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})
	if err := os.Chdir(currentRepo); err != nil {
		t.Fatalf("chdir current repo: %v", err)
	}

	err = vaultValidateImplicitTargetRepoScope(vault.Target{
		File:           filepath.Join(otherRepo, ".env.dev"),
		RepoRoot:       otherRepo,
		FileIsExplicit: false,
	})
	if err == nil {
		t.Fatalf("expected cross-repo implicit target to fail")
	}
	if !strings.Contains(err.Error(), "si vault use --file") {
		t.Fatalf("expected remediation hint in error, got: %v", err)
	}
}

func TestVaultValidateImplicitTargetRepoScopeAllowsExplicitAndOverride(t *testing.T) {
	currentRepo := t.TempDir()
	otherRepo := t.TempDir()
	mustMkdir(t, filepath.Join(currentRepo, "configs"))
	mustMkdir(t, filepath.Join(currentRepo, "agents"))

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})
	if err := os.Chdir(currentRepo); err != nil {
		t.Fatalf("chdir current repo: %v", err)
	}

	// Explicit file selections are always allowed.
	if err := vaultValidateImplicitTargetRepoScope(vault.Target{
		File:           filepath.Join(otherRepo, ".env.dev"),
		RepoRoot:       otherRepo,
		FileIsExplicit: true,
	}); err != nil {
		t.Fatalf("explicit target should be allowed: %v", err)
	}

	// Global override keeps backward-compatible cross-repo defaults.
	t.Setenv("SI_VAULT_ALLOW_CROSS_REPO", "1")
	if err := vaultValidateImplicitTargetRepoScope(vault.Target{
		File:           filepath.Join(otherRepo, ".env.dev"),
		RepoRoot:       otherRepo,
		FileIsExplicit: false,
	}); err != nil {
		t.Fatalf("override should allow cross-repo default: %v", err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
