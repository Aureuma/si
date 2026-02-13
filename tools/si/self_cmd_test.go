package main

import (
	"os"
	"os/exec"
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

func TestSelfBuildBinaryUsesSiblingGoWhenGoNotInPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skip binary build in short mode")
	}
	root, err := resolveSelfRepoRoot("")
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	sysGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("expected go in PATH for test harness: %v", err)
	}
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Place a "go" shim next to output to simulate installer behavior.
	if err := os.Symlink(sysGo, filepath.Join(binDir, "go")); err != nil {
		t.Fatalf("symlink go: %v", err)
	}
	out := filepath.Join(binDir, "si-test")
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "/usr/bin:/bin")
	defer t.Setenv("PATH", origPath)

	if err := selfBuildBinary(root, out, "go", true); err != nil {
		t.Fatalf("selfBuildBinary failed: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected built binary: %v", err)
	}
}

func TestResolveSelfInstallPathExplicit(t *testing.T) {
	got, err := resolveSelfInstallPath("tmp/si")
	if err != nil {
		t.Fatalf("resolveSelfInstallPath failed: %v", err)
	}
	want := filepath.Join(mustGetwd(t), "tmp", "si")
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestResolveSelfInstallPathFromPathLookup(t *testing.T) {
	tmp := t.TempDir()
	siPath := filepath.Join(tmp, "si")
	if err := os.WriteFile(siPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write si shim: %v", err)
	}
	t.Setenv("PATH", tmp)
	got, err := resolveSelfInstallPath("")
	if err != nil {
		t.Fatalf("resolveSelfInstallPath failed: %v", err)
	}
	if got != siPath {
		t.Fatalf("expected %s, got %s", siPath, got)
	}
}

func TestResolveSelfBuildTargetDefaultsToUpgrade(t *testing.T) {
	root, err := resolveSelfRepoRoot("")
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	tmp := t.TempDir()
	siPath := filepath.Join(tmp, "si")
	if err := os.WriteFile(siPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write si shim: %v", err)
	}
	t.Setenv("PATH", tmp)
	got, err := resolveSelfBuildTarget(root, "", "", false)
	if err != nil {
		t.Fatalf("resolveSelfBuildTarget failed: %v", err)
	}
	if !got.Upgrade {
		t.Fatalf("expected upgrade target")
	}
	if got.Target != siPath {
		t.Fatalf("expected target %s, got %s", siPath, got.Target)
	}
}

func TestResolveSelfBuildTargetNoUpgradeDefaultsToRepoBinary(t *testing.T) {
	root, err := resolveSelfRepoRoot("")
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	got, err := resolveSelfBuildTarget(root, "", "", true)
	if err != nil {
		t.Fatalf("resolveSelfBuildTarget failed: %v", err)
	}
	if got.Upgrade {
		t.Fatalf("expected non-upgrade target")
	}
	want := filepath.Join(root, "si")
	if got.Target != want {
		t.Fatalf("expected target %s, got %s", want, got.Target)
	}
}

func TestResolveSelfBuildTargetOutputImpliesNoUpgrade(t *testing.T) {
	root, err := resolveSelfRepoRoot("")
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	got, err := resolveSelfBuildTarget(root, "", "build/si-dev", false)
	if err != nil {
		t.Fatalf("resolveSelfBuildTarget failed: %v", err)
	}
	if got.Upgrade {
		t.Fatalf("expected non-upgrade target")
	}
	want := filepath.Join(mustGetwd(t), "build", "si-dev")
	if got.Target != want {
		t.Fatalf("expected target %s, got %s", want, got.Target)
	}
}

func TestResolveSelfBuildTargetRejectsMixedFlags(t *testing.T) {
	root, err := resolveSelfRepoRoot("")
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	if _, err := resolveSelfBuildTarget(root, "/tmp/si", "", true); err == nil {
		t.Fatalf("expected mixed install-path/no-upgrade to fail")
	}
	if _, err := resolveSelfBuildTarget(root, "/tmp/si", "/tmp/out", false); err == nil {
		t.Fatalf("expected mixed install-path/output to fail")
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return dir
}
