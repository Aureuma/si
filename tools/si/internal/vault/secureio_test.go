package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadFileScoped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.dev")
	want := []byte("A=1\n")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := readFileScoped(path)
	if err != nil {
		t.Fatalf("readFileScoped: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("got %q want %q", string(got), string(want))
	}
}

func TestReadFileScopedEmptyPath(t *testing.T) {
	if _, err := readFileScoped("   "); err == nil {
		t.Fatalf("expected path required error")
	}
}
