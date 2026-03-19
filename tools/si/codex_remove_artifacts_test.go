package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveCodexRemoveArtifactsRequiresRustCLI(t *testing.T) {
	t.Setenv(siRustCLIBinEnv, filepath.Join(t.TempDir(), "missing-si-rs"))

	_, delegated, err := resolveCodexRemoveArtifacts("ferma")
	if err == nil {
		t.Fatalf("expected missing rust cli error")
	}
	if delegated {
		t.Fatalf("unexpected delegated Rust invocation")
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

func TestResolveCodexContainerNameUsesRustPlanWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	payload := `{"name":"ferma","container_name":"si-codex-rust-ferma","slug":"rust-ferma","codex_volume":"si-codex-rust-ferma","gh_volume":"si-gh-rust-ferma"}`
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s' " + shellSingleQuote(payload) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)

	containerName, err := resolveCodexContainerName("ferma")
	if err != nil {
		t.Fatalf("resolveCodexContainerName: %v", err)
	}
	if containerName != "si-codex-rust-ferma" {
		t.Fatalf("unexpected container name %q", containerName)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "codex\nremove-plan\nferma") {
		t.Fatalf("expected codex remove-plan invocation, got %q", string(argsData))
	}
}
