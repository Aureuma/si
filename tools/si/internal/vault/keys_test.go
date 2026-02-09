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
