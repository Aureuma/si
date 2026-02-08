package vault

import (
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
