package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdCodexStopDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	_ = captureOutputForTest(t, func() {
		cmdCodexStop([]string{"ferma"})
	})
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nstop\nferma" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdCodexStartDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >>" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '--' >>" + shellSingleQuote(argsPath) + "\nif [ \"$1\" = \"codex\" ] && [ \"$2\" = \"remove-plan\" ]; then\n  printf '%s\\n' '{\"name\":\"ferma\",\"container_name\":\"si-codex-ferma\",\"slug\":\"ferma\",\"codex_volume\":\"si-codex-ferma\",\"gh_volume\":\"si-github-ferma\"}'\nfi\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	_ = captureOutputForTest(t, func() {
		cmdCodexStart([]string{"ferma"})
	})
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "codex\nstart\nferma") {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdCodexLogsDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	_ = captureOutputForTest(t, func() {
		cmdCodexLogs([]string{"ferma", "--tail", "25"})
	})
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nlogs\nferma\n--tail\n25" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdCodexTailDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	_ = captureOutputForTest(t, func() {
		cmdCodexTail([]string{"ferma", "--tail", "25"})
	})
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\ntail\nferma\n--tail\n25" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdCodexCloneDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >>" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '--' >>" + shellSingleQuote(argsPath) + "\nif [ \"$1\" = \"codex\" ] && [ \"$2\" = \"remove-plan\" ]; then\n  printf '%s\\n' '{\"name\":\"ferma\",\"container_name\":\"si-codex-ferma\",\"slug\":\"ferma\",\"codex_volume\":\"si-codex-ferma\",\"gh_volume\":\"si-github-ferma\"}'\nfi\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output := captureOutputForTest(t, func() {
		cmdCodexClone([]string{"ferma", "acme/repo", "--gh-pat", "tok"})
	})
	if !strings.Contains(output, "repo acme/repo cloned in si-codex-ferma") {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "codex\nclone\nferma\nacme/repo\n--gh-pat\ntok") {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdCodexRemoveDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"name\":\"ferma\",\"container_name\":\"si-codex-ferma\",\"profile_id\":\"\",\"codex_volume\":\"si-codex-ferma\",\"gh_volume\":\"si-gh-ferma\",\"output\":\"removed\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output := captureOutputForTest(t, func() {
		cmdCodexRemove([]string{"ferma"})
	})
	if !strings.Contains(output, "codex container si-codex-ferma removed") {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nremove\nferma\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}
