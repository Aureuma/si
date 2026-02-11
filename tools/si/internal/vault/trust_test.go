package vault

import (
	"path/filepath"
	"testing"
)

func TestTrustStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")

	store := &TrustStore{SchemaVersion: 3}
	store.Upsert(TrustEntry{
		RepoRoot:    "/repo",
		File:        "/repo/.env",
		Fingerprint: "deadbeef",
	})
	if err := store.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadTrustStore(path)
	if err != nil {
		t.Fatalf("LoadTrustStore: %v", err)
	}
	e, ok := loaded.Find("/repo", "/repo/.env")
	if !ok {
		t.Fatalf("expected entry")
	}
	if e.Fingerprint != "deadbeef" {
		t.Fatalf("fingerprint=%q", e.Fingerprint)
	}
	if !loaded.Delete("/repo", "/repo/.env") {
		t.Fatalf("expected delete")
	}
	if err := loaded.Save(path); err != nil {
		t.Fatalf("Save2: %v", err)
	}
}
