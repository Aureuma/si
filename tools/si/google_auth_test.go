package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoogleAccountEnvPrefix(t *testing.T) {
	if got := googleAccountEnvPrefix("core-main", GoogleAccountEntry{}); got != "GOOGLE_CORE_MAIN_" {
		t.Fatalf("unexpected prefix: %q", got)
	}
	if got := googleAccountEnvPrefix("core", GoogleAccountEntry{VaultPrefix: "google_ops"}); got != "GOOGLE_OPS_" {
		t.Fatalf("unexpected vault prefix: %q", got)
	}
}

func TestResolveGooglePlacesAPIKeyByEnvironment(t *testing.T) {
	t.Setenv("GOOGLE_CORE_STAGING_PLACES_API_KEY", "stage-key")
	value, source := resolveGooglePlacesAPIKey("core", GoogleAccountEntry{}, "staging", "")
	if value != "stage-key" {
		t.Fatalf("unexpected key: %q", value)
	}
	if source != "env:GOOGLE_CORE_STAGING_PLACES_API_KEY" {
		t.Fatalf("unexpected source: %q", source)
	}
}

func TestParseGoogleEnvironment(t *testing.T) {
	if _, err := parseGoogleEnvironment("test"); err == nil {
		t.Fatalf("expected error for test environment")
	}
	if env, err := parseGoogleEnvironment("prod"); err != nil || env != "prod" {
		t.Fatalf("unexpected parse result: env=%q err=%v", env, err)
	}
}

func TestCmdGooglePlacesContextListDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"contexts\":[{\"alias\":\"core\"}]}'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siRustCLILegacyToggleEnv, "")

	out := captureOutputForTest(t, func() {
		cmdGooglePlacesContextList([]string{"--json"})
	})

	if !strings.Contains(out, "\"alias\":\"core\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "google\nplaces\ncontext\nlist\n--json" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdGooglePlacesContextCurrentDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"account_alias\":\"core\",\"project_id\":\"proj_core\",\"environment\":\"prod\",\"language_code\":\"en\",\"region_code\":\"US\",\"source\":\"env:CORE_API_KEY,env:CORE_PROJECT\",\"base_url\":\"https://places.googleapis.com\"}'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siRustCLILegacyToggleEnv, "")

	out := captureOutputForTest(t, func() {
		cmdGooglePlacesContextCurrent([]string{"--json"})
	})

	if !strings.Contains(out, "\"account_alias\":\"core\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "google\nplaces\ncontext\ncurrent\n--json" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdGooglePlacesAuthStatusDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"account_alias\":\"core\",\"project_id\":\"proj_core\",\"environment\":\"prod\",\"language_code\":\"en\",\"region_code\":\"US\",\"source\":\"env:CORE_API_KEY,env:CORE_PROJECT\",\"key_preview\":\"AIza*******xyz\",\"base_url\":\"https://places.googleapis.com\"}'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siRustCLILegacyToggleEnv, "")

	out := captureOutputForTest(t, func() {
		cmdGooglePlacesAuthStatus([]string{"--json"})
	})

	if !strings.Contains(out, "\"account_alias\":\"core\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "google\nplaces\nauth\nstatus\n--json" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}
