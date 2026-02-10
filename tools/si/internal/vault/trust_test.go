package vault

import (
	"path/filepath"
	"testing"
)

func TestTrustStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")

	store := &TrustStore{SchemaVersion: 2}
	store.Upsert(TrustEntry{
		RepoRoot:    "/repo",
		VaultDir:    "/repo/vault",
		File:        "/repo/vault/.env",
		VaultRepo:   "git@example.com:org/vault.git",
		Fingerprint: "deadbeef",
	})
	if err := store.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadTrustStore(path)
	if err != nil {
		t.Fatalf("LoadTrustStore: %v", err)
	}
	e, ok := loaded.Find("/repo", "/repo/vault/.env")
	if !ok {
		t.Fatalf("expected entry")
	}
	if e.Fingerprint != "deadbeef" {
		t.Fatalf("fingerprint=%q", e.Fingerprint)
	}
	if !loaded.Delete("/repo", "/repo/vault/.env") {
		t.Fatalf("expected delete")
	}
	if err := loaded.Save(path); err != nil {
		t.Fatalf("Save2: %v", err)
	}
}
