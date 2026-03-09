package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityFileSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "age.key")

	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	secret := id.String()
	if err := saveIdentityToFile(path, secret); err != nil {
		t.Fatalf("saveIdentityToFile: %v", err)
	}
	loaded, err := loadIdentityFromFile(path)
	if err != nil {
		t.Fatalf("loadIdentityFromFile: %v", err)
	}
	if loaded.String() != secret {
		t.Fatalf("identity mismatch: got %q want %q", loaded.String(), secret)
	}
}

func TestLoadIdentityFromFileRejectsInsecurePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "age.key")
	if err := os.WriteFile(path, []byte("AGE-SECRET-KEY-1QQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQ\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Ensure perms are actually insecure on this fs.
	_ = os.Chmod(path, 0o644)
	_, err := loadIdentityFromFile(path)
	if err == nil {
		t.Fatalf("expected permission error")
	}
}

func TestLoadIdentityFromFileOverrideRequiresTruthyValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "age.key")
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	if err := os.WriteFile(path, []byte(id.String()+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Setenv("SI_VAULT_ALLOW_INSECURE_KEY_FILE", "0")
	if _, err := loadIdentityFromFile(path); err == nil {
		t.Fatalf("expected rejection when override is non-truthy")
	}

	t.Setenv("SI_VAULT_ALLOW_INSECURE_KEY_FILE", "1")
	if _, err := loadIdentityFromFile(path); err != nil {
		t.Fatalf("expected truthy override to allow load: %v", err)
	}
}

func TestLoadIdentityIgnoresLegacyIdentityEnvVars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "age.key")
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	if err := saveIdentityToFile(path, id.String()); err != nil {
		t.Fatalf("saveIdentityToFile: %v", err)
	}
	t.Setenv("SI_VAULT_IDENTITY", "not-a-valid-age-key")
	t.Setenv("SI_VAULT_PRIVATE_KEY", "not-a-valid-age-key")
	t.Setenv("SI_VAULT_IDENTITY_FILE", filepath.Join(dir, "missing.key"))

	info, err := LoadIdentity(KeyConfig{Backend: "file", KeyFile: path})
	if err != nil {
		t.Fatalf("LoadIdentity(file): %v", err)
	}
	if info == nil || info.Identity == nil {
		t.Fatalf("expected identity info from file backend")
	}
	if info.Source != "file" {
		t.Fatalf("identity source=%q want file", info.Source)
	}
}
