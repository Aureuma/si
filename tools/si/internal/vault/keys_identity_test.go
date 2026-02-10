package vault

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureIdentityFileCreatesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "age.key")

	info, created, err := EnsureIdentity(KeyConfig{Backend: "file", KeyFile: keyFile})
	if err != nil {
		t.Fatalf("EnsureIdentity: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true")
	}
	if info == nil || info.Identity == nil {
		t.Fatalf("expected identity")
	}
	if info.Source != "file" {
		t.Fatalf("source=%q want %q", info.Source, "file")
	}
	if _, err := os.Stat(keyFile); err != nil {
		t.Fatalf("stat keyfile: %v", err)
	}
}

func TestEnsureIdentityFileRefusesInvalidExisting(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "age.key")
	if err := os.WriteFile(keyFile, []byte("not-a-key\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, _, err := EnsureIdentity(KeyConfig{Backend: "file", KeyFile: keyFile})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, ErrIdentityInvalid) {
		t.Fatalf("expected ErrIdentityInvalid, got: %v", err)
	}
}

func TestRotateIdentityFileOverwrites(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "age.key")
	if err := os.WriteFile(keyFile, []byte("not-a-key\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, err := RotateIdentity(KeyConfig{Backend: "file", KeyFile: keyFile})
	if err != nil {
		t.Fatalf("RotateIdentity: %v", err)
	}
	if info == nil || info.Identity == nil {
		t.Fatalf("expected identity")
	}

	// Now LoadIdentity should succeed.
	got, err := LoadIdentity(KeyConfig{Backend: "file", KeyFile: keyFile})
	if err != nil {
		t.Fatalf("LoadIdentity: %v", err)
	}
	if got.Identity.Recipient().String() != info.Identity.Recipient().String() {
		t.Fatalf("recipient mismatch after rotate")
	}
}
