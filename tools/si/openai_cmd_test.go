package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdOpenAIContextListDelegatesToRustCLIWhenConfigured(t *testing.T) {
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
		cmdOpenAIContextList([]string{"--json"})
	})

	if !strings.Contains(out, "\"alias\":\"core\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\ncontext\nlist\n--json" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdOpenAIContextCurrentDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"account_alias\":\"core\",\"base_url\":\"https://api.openai.com\",\"organization_id\":\"org_123\",\"project_id\":\"proj_123\",\"source\":\"env:OPENAI_CORE_API_KEY,settings.default_project_id\",\"admin_key_set\":true}'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		cmdOpenAIContextCurrent([]string{"--json"})
	})

	if !strings.Contains(out, "\"account_alias\":\"core\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\ncontext\ncurrent\n--json" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdOpenAIModelListDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"status_code\":200,\"status\":\"200 OK\",\"request_id\":\"req_123\",\"body\":\"{\\\"data\\\":[]}\",\"data\":{\"data\":[]}}'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		cmdOpenAIModelList([]string{"--json", "--limit", "1"})
	})

	if !strings.Contains(out, "\"status_code\":200") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nmodel\nlist\n--json\n--limit\n1" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdOpenAIModelGetDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'Status: 200 200 OK\\nRequest ID: req_123\\n{\\n  \"id\": \"gpt-test\"\\n}'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		cmdOpenAIModelGet([]string{"gpt-test"})
	})

	if !strings.Contains(out, "Status: 200 200 OK") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nmodel\nget\ngpt-test" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}
