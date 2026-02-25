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

func TestResolveVaultSyncBackendDefaultsAndNoLegacyAutoMode(t *testing.T) {
	settings := Settings{}
	applySettingsDefaults(&settings)

	got, err := resolveVaultSyncBackend(settings)
	if err != nil {
		t.Fatalf("resolve default backend: %v", err)
	}
	if got.Mode != vaultSyncBackendSun || got.Source != "default" {
		t.Fatalf("unexpected default resolution: %+v", got)
	}

	settings.Sun.AutoSync = true
	settings.Vault.SyncBackend = ""
	got, err = resolveVaultSyncBackend(settings)
	if err != nil {
		t.Fatalf("resolve backend with sun.auto_sync fallback removed: %v", err)
	}
	if got.Mode != vaultSyncBackendSun || got.Source != "default" {
		t.Fatalf("unexpected resolution when backend unset: %+v", got)
	}
}

func TestResolveVaultSyncBackendOverridesAndValidation(t *testing.T) {
	settings := Settings{}
	applySettingsDefaults(&settings)
	settings.Vault.SyncBackend = "sun"

	got, err := resolveVaultSyncBackend(settings)
	if err != nil {
		t.Fatalf("resolve settings backend: %v", err)
	}
	if got.Mode != vaultSyncBackendSun || got.Source != "settings" {
		t.Fatalf("unexpected settings resolution: %+v", got)
	}

	settings.Vault.SyncBackend = "sun"
	got, err = resolveVaultSyncBackend(settings)
	if err != nil {
		t.Fatalf("resolve settings backend sun alias: %v", err)
	}
	if got.Mode != vaultSyncBackendSun || got.Source != "settings" {
		t.Fatalf("unexpected settings sun-alias resolution: %+v", got)
	}

	settings.Vault.SyncBackend = "dual"
	got, err = resolveVaultSyncBackend(settings)
	if err != nil {
		t.Fatalf("resolve settings backend dual alias: %v", err)
	}
	if got.Mode != vaultSyncBackendSun || got.Source != "settings" {
		t.Fatalf("unexpected settings dual-alias resolution: %+v", got)
	}

	t.Setenv("SI_VAULT_SYNC_BACKEND", "git")
	got, err = resolveVaultSyncBackend(settings)
	if err != nil {
		t.Fatalf("resolve env backend: %v", err)
	}
	if got.Mode != vaultSyncBackendSun || got.Source != "env" {
		t.Fatalf("unexpected env resolution: %+v", got)
	}

	t.Setenv("SI_VAULT_SYNC_BACKEND", "sun")
	got, err = resolveVaultSyncBackend(settings)
	if err != nil {
		t.Fatalf("resolve env backend sun alias: %v", err)
	}
	if got.Mode != vaultSyncBackendSun || got.Source != "env" {
		t.Fatalf("unexpected env sun-alias resolution: %+v", got)
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

func TestVaultNormalizeScopeRespectsSunObjectKeyLimit(t *testing.T) {
	raw := strings.Repeat("a", 300)
	scope := vaultNormalizeScope(raw)
	if len(scope) > maxVaultScopeLen {
		t.Fatalf("scope length=%d exceeds max=%d", len(scope), maxVaultScopeLen)
	}
	kind := "vault_kv." + scope
	if len(kind) > 128 {
		t.Fatalf("vault kind length=%d exceeds server key limit", len(kind))
	}
}

func TestVaultNormalizeScopePreservesRepoEnvNamespace(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "Aureuma/Dev", want: "aureuma/dev"},
		{raw: "releasemind//prod", want: "releasemind/prod"},
		{raw: "shared/Prod@", want: "shared/prod"},
		{raw: ".env.dev", want: "dev"},
		{raw: "C:\\repo\\.env.prod", want: "prod"},
	}
	for _, tc := range tests {
		if got := vaultNormalizeScope(tc.raw); got != tc.want {
			t.Fatalf("vaultNormalizeScope(%q) = %q; want %q", tc.raw, got, tc.want)
		}
	}
}

func TestVaultSunKVKindForScopeCompatibilityAndNamespacedEncoding(t *testing.T) {
	legacyScope := "legacy-scope"
	if got := vaultSunKVKindForScope(legacyScope); got != "vault_kv."+legacyScope {
		t.Fatalf("legacy scope kind mismatch: got %q", got)
	}

	kind := vaultSunKVKindForScope("aureuma/dev")
	if strings.Contains(kind, "/") {
		t.Fatalf("kind must not contain slash: %q", kind)
	}
	if !strings.HasPrefix(kind, "vault_kv.aureuma.dev.") {
		t.Fatalf("unexpected namespaced kind format: %q", kind)
	}
	if len(kind) > 128 {
		t.Fatalf("kind length=%d exceeds max 128", len(kind))
	}

	k1 := vaultSunKVKindForScope("aureuma/dev")
	k2 := vaultSunKVKindForScope("aureuma/prod")
	if k1 == k2 {
		t.Fatalf("different namespaced scopes must map to different kinds")
	}
}
