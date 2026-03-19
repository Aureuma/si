package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdAppleAppStoreContextListDelegatesToRustCLIWhenConfigured(t *testing.T) {
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
		cmdAppleAppStoreContextList([]string{"--json"})
	})

	if !strings.Contains(out, "\"alias\":\"core\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "apple\nappstore\ncontext\nlist\n--json" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdAppleAppStoreContextCurrentDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"account_alias\":\"core\",\"project_id\":\"proj_core\",\"environment\":\"prod\",\"source\":\"settings.apple.project_id\",\"token_source\":\"env:CORE_PRIVATE_KEY\",\"bundle_id\":\"com.example.core\",\"locale\":\"en-US\",\"platform\":\"IOS\",\"base_url\":\"https://api.appstoreconnect.apple.com\"}'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siRustCLILegacyToggleEnv, "")

	out := captureOutputForTest(t, func() {
		cmdAppleAppStoreContextCurrent([]string{"--json"})
	})

	if !strings.Contains(out, "\"account_alias\":\"core\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "apple\nappstore\ncontext\ncurrent\n--json" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}
