package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveInstallPathDefaultNonRoot(t *testing.T) {
	cfg := config{}
	path, err := resolveInstallPath(cfg)
	if err != nil {
		t.Fatalf("resolveInstallPath: %v", err)
	}
	if filepath.Base(path) != "si" {
		t.Fatalf("unexpected install path: %s", path)
	}
}

func TestValidateSourceDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tools", "si"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tools", "si", "go.mod"), []byte("module si/tools/si\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := validateSourceDir(dir); err != nil {
		t.Fatalf("validateSourceDir: %v", err)
	}
}

func TestValidateSourceDirFails(t *testing.T) {
	dir := t.TempDir()
	if err := validateSourceDir(dir); err == nil {
		t.Fatalf("expected validateSourceDir to fail")
	}
}
