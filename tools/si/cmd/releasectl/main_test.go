package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestMoveFileMovesWithinFilesystem(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o640); err != nil {
		t.Fatalf("write src: %v", err)
	}

	if err := moveFile(src, dst); err != nil {
		t.Fatalf("moveFile: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("expected src removed, got err=%v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("unexpected dst contents: %q", got)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat dst: %v", err)
	}
	if info.Mode().Perm() != fs.FileMode(0o640) {
		t.Fatalf("unexpected dst mode: %v", info.Mode().Perm())
	}
}
