package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdGCPContextListDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"contexts\":[{\"alias\":\"core\"}]}'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		cmdGCPContextList([]string{"--json"})
	})

	if !strings.Contains(out, "\"alias\":\"core\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "gcp\ncontext\nlist\n--json" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdGCPContextCurrentDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"account_alias\":\"core\",\"environment\":\"prod\",\"project_id\":\"proj_core\",\"base_url\":\"https://serviceusage.googleapis.com\",\"source\":\"settings.gcp.project_id,env:CORE_GCP_ACCESS_TOKEN\"}'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		cmdGCPContextCurrent([]string{"--json"})
	})

	if !strings.Contains(out, "\"account_alias\":\"core\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "gcp\ncontext\ncurrent\n--json" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdGCPAuthStatusDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"account_alias\":\"core\",\"environment\":\"prod\",\"project_id\":\"proj_core\",\"base_url\":\"https://serviceusage.googleapis.com\",\"source\":\"settings.gcp.project_id,env:CORE_GCP_ACCESS_TOKEN\",\"token_preview\":\"ya2***xyz\"}'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		cmdGCPAuthStatus([]string{"--json"})
	})

	if !strings.Contains(out, "\"account_alias\":\"core\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "gcp\nauth\nstatus\n--json" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}
