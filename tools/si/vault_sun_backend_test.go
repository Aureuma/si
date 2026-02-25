package main

import (
	"os"
	"path/filepath"
	"testing"

	"si/tools/si/internal/vault"
)

func TestVaultHydrateFromSunDoesNotMaterializeLocalFile(t *testing.T) {
	settings := Settings{}
	applySettingsDefaults(&settings)
	settings.Vault.SyncBackend = vaultSyncBackendSun

	identity, err := vault.GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	t.Setenv("SI_VAULT_IDENTITY", identity.String())

	tmp := t.TempDir()
	blockedDir := filepath.Join(tmp, "blocked")
	if err := os.MkdirAll(blockedDir, 0o500); err != nil {
		t.Fatalf("MkdirAll blockedDir: %v", err)
	}
	targetPath := filepath.Join(blockedDir, "sun.vault")
	target := vault.Target{
		CWD:      tmp,
		RepoRoot: tmp,
		File:     targetPath,
	}

	if err := vaultHydrateFromSun(settings, target, false); err != nil {
		t.Fatalf("vaultHydrateFromSun: %v", err)
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		if err == nil {
			t.Fatalf("expected no local file materialization at %s", targetPath)
		}
		t.Fatalf("stat target path: %v", err)
	}
}
