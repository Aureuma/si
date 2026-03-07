package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSIVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "version.go")
	content := "package main\n\nconst siVersion = \"1.2.3\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
	version, err := resolveSIVersion(path)
	if err != nil {
		t.Fatalf("resolveSIVersion: %v", err)
	}
	if version != "1.2.3" {
		t.Fatalf("unexpected version %q", version)
	}
}

func TestResolveSIVersionFailsWithoutConstant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "version.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
	if _, err := resolveSIVersion(path); err == nil {
		t.Fatalf("expected resolveSIVersion to fail")
	}
}

func TestFindNpmPackageTarball(t *testing.T) {
	dir := t.TempDir()
	one := filepath.Join(dir, "aureuma-si-2.0.0.tgz")
	two := filepath.Join(dir, "aureuma-si-1.9.0.tgz")
	if err := os.WriteFile(one, []byte("x"), 0o644); err != nil {
		t.Fatalf("write package one: %v", err)
	}
	if err := os.WriteFile(two, []byte("x"), 0o644); err != nil {
		t.Fatalf("write package two: %v", err)
	}
	got, err := findNpmPackageTarball(dir)
	if err != nil {
		t.Fatalf("findNpmPackageTarball: %v", err)
	}
	if got != two {
		t.Fatalf("expected lexicographically first tarball %q, got %q", two, got)
	}
}

func TestFindNpmPackageTarballFailsWhenMissing(t *testing.T) {
	if _, err := findNpmPackageTarball(t.TempDir()); err == nil {
		t.Fatalf("expected missing tarball error")
	}
}
