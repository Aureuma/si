package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSelfRepoRoot(t *testing.T) {
	root, err := resolveSelfRepoRoot("")
	if err != nil {
		t.Fatalf("resolveSelfRepoRoot failed: %v", err)
	}
	if !exists(filepath.Join(root, "configs")) || !exists(filepath.Join(root, "agents")) {
		t.Fatalf("resolved root is not si repo root: %s", root)
	}
}

func TestResolveSelfRepoRootInvalid(t *testing.T) {
	if _, err := resolveSelfRepoRoot("/tmp/does-not-exist-si-root"); err == nil {
		t.Fatalf("expected invalid repo path to fail")
	}
}

func TestSelfBuildBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("skip binary build in short mode")
	}
	root, err := resolveSelfRepoRoot("")
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	out := filepath.Join(t.TempDir(), "si-test")
	if err := selfBuildBinary(root, out, "go", true); err != nil {
		t.Fatalf("selfBuildBinary failed: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("built binary stat failed: %v", err)
	}
	if info.IsDir() {
		t.Fatalf("expected file, got directory")
	}
}
