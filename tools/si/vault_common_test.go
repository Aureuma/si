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

func TestResolveVaultSyncBackendDefaultsAndLegacy(t *testing.T) {
	settings := Settings{}
	applySettingsDefaults(&settings)

	got, err := resolveVaultSyncBackend(settings)
	if err != nil {
		t.Fatalf("resolve default backend: %v", err)
	}
	if got.Mode != vaultSyncBackendGit || got.Source != "default" {
		t.Fatalf("unexpected default resolution: %+v", got)
	}

	settings.Helia.AutoSync = true
	settings.Vault.SyncBackend = ""
	got, err = resolveVaultSyncBackend(settings)
	if err != nil {
		t.Fatalf("resolve legacy backend: %v", err)
	}
	if got.Mode != vaultSyncBackendDual || got.Source != "legacy_helia_auto_sync" {
		t.Fatalf("unexpected legacy resolution: %+v", got)
	}
}

func TestResolveVaultSyncBackendOverridesAndValidation(t *testing.T) {
	settings := Settings{}
	applySettingsDefaults(&settings)
	settings.Vault.SyncBackend = "helia"

	got, err := resolveVaultSyncBackend(settings)
	if err != nil {
		t.Fatalf("resolve settings backend: %v", err)
	}
	if got.Mode != vaultSyncBackendHelia || got.Source != "settings" {
		t.Fatalf("unexpected settings resolution: %+v", got)
	}

	t.Setenv("SI_VAULT_SYNC_BACKEND", "git")
	got, err = resolveVaultSyncBackend(settings)
	if err != nil {
		t.Fatalf("resolve env backend: %v", err)
	}
	if got.Mode != vaultSyncBackendGit || got.Source != "env" {
		t.Fatalf("unexpected env resolution: %+v", got)
	}

	t.Setenv("SI_VAULT_SYNC_BACKEND", "invalid")
	if _, err := resolveVaultSyncBackend(settings); err == nil {
		t.Fatalf("expected invalid env backend to fail")
	}

	t.Setenv("SI_VAULT_SYNC_BACKEND", "")
	settings.Vault.SyncBackend = "invalid"
	if _, err := resolveVaultSyncBackend(settings); err == nil {
		t.Fatalf("expected invalid settings backend to fail")
	}
}
