package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveGoCompatToolchainPrefersExplicitBinary(t *testing.T) {
	dir := t.TempDir()
	goPath := filepath.Join(dir, "custom-go")
	if err := os.WriteFile(goPath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatalf("write go binary: %v", err)
	}

	got, err := resolveGoCompatToolchain(filepath.Join(dir, "si"), goPath)
	if err != nil {
		t.Fatalf("resolveGoCompatToolchain() error = %v", err)
	}
	if got != goPath {
		t.Fatalf("expected %q, got %q", goPath, got)
	}
}

func TestResolveGoCompatToolchainUsesSiblingGoWhenPathMissing(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}
	sibling := filepath.Join(binDir, "go")
	if err := os.WriteFile(sibling, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatalf("write sibling go: %v", err)
	}

	t.Setenv("PATH", t.TempDir())

	got, err := resolveGoCompatToolchain(filepath.Join(binDir, "si"), "go")
	if err != nil {
		t.Fatalf("resolveGoCompatToolchain() error = %v", err)
	}
	if got != sibling {
		t.Fatalf("expected sibling go %q, got %q", sibling, got)
	}
}

func TestResolveGoCompatToolchainReturnsHelpfulErrorWithoutBootstrap(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := resolveGoCompatToolchain(filepath.Join(t.TempDir(), "si"), "go")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "install Go 1.25+") {
		t.Fatalf("unexpected error: %v", err)
	}
}
