package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveCodexRemoveArtifactsFallsBackToGoArtifacts(t *testing.T) {
	t.Setenv(siRustCLIBinEnv, "")
	t.Setenv(siExperimentalRustCLIEnv, "")

	artifacts, delegated, err := resolveCodexRemoveArtifacts("ferma")
	if err != nil {
		t.Fatalf("resolveCodexRemoveArtifacts: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go fallback artifacts")
	}
	if artifacts == nil {
		t.Fatalf("expected fallback artifacts")
	}
	if artifacts.ContainerName != "si-codex-ferma" {
		t.Fatalf("unexpected container name %q", artifacts.ContainerName)
	}
	if artifacts.CodexVolume != "si-codex-ferma" || artifacts.GHVolume != "si-gh-ferma" {
		t.Fatalf("unexpected volumes %#v", artifacts)
	}
}

func TestResolveCodexRemoveArtifactsUsesRustPlanWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	payload := `{"name":"ferma","container_name":"si-codex-rust-ferma","slug":"rust-ferma","codex_volume":"si-codex-rust-ferma","gh_volume":"si-gh-rust-ferma"}`
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s' " + shellSingleQuote(payload) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	artifacts, delegated, err := resolveCodexRemoveArtifacts("ferma")
	if err != nil {
		t.Fatalf("resolveCodexRemoveArtifacts: %v", err)
	}
	if !delegated {
		t.Fatalf("expected Rust remove plan delegation")
	}
	if artifacts == nil || artifacts.ContainerName != "si-codex-rust-ferma" || artifacts.CodexVolume != "si-codex-rust-ferma" {
		t.Fatalf("unexpected Rust artifacts %#v", artifacts)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "codex\nremove-plan\nferma") {
		t.Fatalf("expected codex remove-plan invocation, got %q", string(argsData))
	}
}
