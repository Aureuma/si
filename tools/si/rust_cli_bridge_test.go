package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunVersionCommandDefaultsToGoVersion(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	out := captureOutputForTest(t, func() {
		if err := runVersionCommand(); err != nil {
			t.Fatalf("runVersionCommand: %v", err)
		}
	})

	if strings.TrimSpace(out) != siVersion {
		t.Fatalf("expected Go version output %q, got %q", siVersion, out)
	}
}

func TestRunVersionCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'v-rust-bridge'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		if err := runVersionCommand(); err != nil {
			t.Fatalf("runVersionCommand: %v", err)
		}
	})

	if strings.TrimSpace(out) != "v-rust-bridge" {
		t.Fatalf("expected delegated Rust output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "version" {
		t.Fatalf("expected Rust CLI args to be 'version', got %q", string(argsData))
	}
}

func TestMaybeDispatchRustCLIReadOnlyErrorsWhenConfiguredBinaryMissing(t *testing.T) {
	t.Setenv(siRustCLIBinEnv, filepath.Join(t.TempDir(), "missing-si-rs"))

	delegated, err := maybeDispatchRustCLIReadOnly("version")
	if err == nil {
		t.Fatalf("expected missing explicit Rust CLI binary to fail")
	}
	if delegated {
		t.Fatalf("expected delegated=false on failure")
	}
	if !strings.Contains(err.Error(), siRustCLIBinEnv) {
		t.Fatalf("expected error to mention %s, got %v", siRustCLIBinEnv, err)
	}
}

func TestRunVersionCommandUsesRepoBuiltRustBinaryWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	binPath := filepath.Join(dir, ".artifacts", "cargo-target", "debug", "si-rs")
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' 'v-rust-repo'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(binPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	origRepoRoot := rustCLIRepoRoot
	origLookPath := rustCLILookPath
	t.Cleanup(func() {
		rustCLIRepoRoot = origRepoRoot
		rustCLILookPath = origLookPath
	})
	rustCLIRepoRoot = func() (string, error) { return dir, nil }
	rustCLILookPath = func(file string) (string, error) { return "", os.ErrNotExist }

	t.Setenv(siExperimentalRustCLIEnv, "1")
	t.Setenv(siRustCLIBinEnv, "")

	out := captureOutputForTest(t, func() {
		if err := runVersionCommand(); err != nil {
			t.Fatalf("runVersionCommand: %v", err)
		}
	})

	if strings.TrimSpace(out) != "v-rust-repo" {
		t.Fatalf("expected repo Rust output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "version" {
		t.Fatalf("expected Rust CLI args to be 'version', got %q", string(argsData))
	}
}

func TestRunHelpCommandDefaultsToGoUsage(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	out := captureOutputForTest(t, func() {
		if err := runHelpCommand(nil); err != nil {
			t.Fatalf("runHelpCommand: %v", err)
		}
	})

	if !strings.Contains(out, "Holistic CLI for si.") {
		t.Fatalf("expected Go usage output, got %q", out)
	}
}

func TestRunHelpCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-help'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		if err := runHelpCommand([]string{"remote-control"}); err != nil {
			t.Fatalf("runHelpCommand: %v", err)
		}
	})

	if strings.TrimSpace(out) != "rust-help" {
		t.Fatalf("expected delegated Rust help output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "help\nremote-control" {
		t.Fatalf("expected Rust CLI args to be help + remote-control, got %q", string(argsData))
	}
}

func TestRunProvidersCharacteristicsCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runProvidersCharacteristicsCommand([]string{"--provider", "github", "--json"})
	if err != nil {
		t.Fatalf("runProvidersCharacteristicsCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go providers characteristics path by default")
	}
}

func TestRunProvidersCharacteristicsCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-providers'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runProvidersCharacteristicsCommand([]string{"--provider", "github", "--json"})
		if err != nil {
			t.Fatalf("runProvidersCharacteristicsCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected providers characteristics to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-providers" {
		t.Fatalf("expected delegated Rust providers output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "providers\ncharacteristics\n--provider\ngithub\n--json" {
		t.Fatalf("expected Rust CLI args to be providers characteristics + flags, got %q", string(argsData))
	}
}

func TestRunCloudflareContextListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runCloudflareContextListCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runCloudflareContextListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go cloudflare context list path by default")
	}
}

func TestRunCloudflareContextListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-cloudflare-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runCloudflareContextListCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runCloudflareContextListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected cloudflare context list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-cloudflare-list" {
		t.Fatalf("expected delegated Rust cloudflare context list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "cloudflare\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be cloudflare context list + flags, got %q", string(argsData))
	}
}

func TestRunCloudflareContextCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runCloudflareContextCommand([]string{"list", "--json"})
	if err != nil {
		t.Fatalf("runCloudflareContextCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go cloudflare context path by default")
	}
}

func TestRunCloudflareContextCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-cloudflare-context-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runCloudflareContextCommand([]string{"list", "--json"})
		if err != nil {
			t.Fatalf("runCloudflareContextCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected cloudflare context to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-cloudflare-context-wrapper" {
		t.Fatalf("expected delegated Rust cloudflare context output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "cloudflare\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be cloudflare context + args, got %q", string(argsData))
	}
}

func TestRunCloudflareContextCurrentCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runCloudflareContextCurrentCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runCloudflareContextCurrentCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go cloudflare context current path by default")
	}
}

func TestRunCloudflareContextCurrentCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-cloudflare-current'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runCloudflareContextCurrentCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runCloudflareContextCurrentCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected cloudflare context current to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-cloudflare-current" {
		t.Fatalf("expected delegated Rust cloudflare context current output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "cloudflare\ncontext\ncurrent\n--json" {
		t.Fatalf("expected Rust CLI args to be cloudflare context current + flags, got %q", string(argsData))
	}
}

func TestRunCloudflareAuthCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runCloudflareAuthCommand([]string{"status", "--json"})
	if err != nil {
		t.Fatalf("runCloudflareAuthCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go cloudflare auth path by default")
	}
}

func TestRunCloudflareAuthCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-cloudflare-auth-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runCloudflareAuthCommand([]string{"status", "--json"})
		if err != nil {
			t.Fatalf("runCloudflareAuthCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected cloudflare auth to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-cloudflare-auth-wrapper" {
		t.Fatalf("expected delegated Rust cloudflare auth output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "cloudflare\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be cloudflare auth + args, got %q", string(argsData))
	}
}

func TestRunCloudflareAuthStatusCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runCloudflareAuthStatusCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runCloudflareAuthStatusCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go cloudflare auth status path by default")
	}
}

func TestRunCloudflareAuthStatusCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-cloudflare-auth'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runCloudflareAuthStatusCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runCloudflareAuthStatusCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected cloudflare auth status to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-cloudflare-auth" {
		t.Fatalf("expected delegated Rust cloudflare auth status output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "cloudflare\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be cloudflare auth status + flags, got %q", string(argsData))
	}
}

func TestRunCloudflareCommandDefaultsToGoForNonMigratedSubtree(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runCloudflareCommand([]string{"zone", "list", "--json"})
	if err != nil {
		t.Fatalf("runCloudflareCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go cloudflare root path for non-migrated subtree")
	}
}

func TestRunCloudflareCommandDelegatesToRustCLIForMigratedReadPath(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-cloudflare-root'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runCloudflareCommand([]string{"context", "list", "--json"})
		if err != nil {
			t.Fatalf("runCloudflareCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected cloudflare root to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-cloudflare-root" {
		t.Fatalf("expected delegated Rust cloudflare root output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "cloudflare\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be cloudflare root + args, got %q", string(argsData))
	}
}

func TestRunAppleAppStoreContextListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runAppleAppStoreContextListCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runAppleAppStoreContextListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go apple appstore context list path by default")
	}
}

func TestRunAppleAppStoreContextListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-apple-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runAppleAppStoreContextListCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runAppleAppStoreContextListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected apple appstore context list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-apple-list" {
		t.Fatalf("expected delegated Rust apple appstore context list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "apple\nappstore\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be apple appstore context list + flags, got %q", string(argsData))
	}
}

func TestRunAppleAppStoreContextCurrentCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runAppleAppStoreContextCurrentCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runAppleAppStoreContextCurrentCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go apple appstore context current path by default")
	}
}

func TestRunAppleAppStoreContextCurrentCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-apple-current'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runAppleAppStoreContextCurrentCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runAppleAppStoreContextCurrentCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected apple appstore context current to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-apple-current" {
		t.Fatalf("expected delegated Rust apple appstore context current output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "apple\nappstore\ncontext\ncurrent\n--json" {
		t.Fatalf("expected Rust CLI args to be apple appstore context current + flags, got %q", string(argsData))
	}
}

func TestRunAppleAppStoreAuthStatusCommandDefaultsToGoWhileVerifyIsEnabled(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runAppleAppStoreAuthStatusCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runAppleAppStoreAuthStatusCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go apple appstore auth status path while verification stays on the Go side")
	}
}

func TestRunAppleAppStoreAuthStatusCommandDelegatesToRustCLIWhenVerifyDisabled(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-apple-auth'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runAppleAppStoreAuthStatusCommand([]string{"--verify=false", "--json"})
		if err != nil {
			t.Fatalf("runAppleAppStoreAuthStatusCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected apple appstore auth status to delegate to Rust when verification is disabled")
		}
	})

	if strings.TrimSpace(out) != "rust-apple-auth" {
		t.Fatalf("expected delegated Rust apple appstore auth status output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "apple\nappstore\nauth\nstatus\n--verify=false\n--json" {
		t.Fatalf("expected Rust CLI args to be apple appstore auth status + flags, got %q", string(argsData))
	}
}

func TestRunAppleAppStoreAuthCommandDefaultsToGoWhileVerifyIsEnabled(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runAppleAppStoreAuthCommand([]string{"status", "--json"})
	if err != nil {
		t.Fatalf("runAppleAppStoreAuthCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go apple appstore auth path while verify is enabled")
	}
}

func TestRunAppleAppStoreAuthCommandDelegatesToRustCLIWhenVerifyDisabled(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-apple-appstore-auth'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runAppleAppStoreAuthCommand([]string{"status", "--verify=false", "--json"})
		if err != nil {
			t.Fatalf("runAppleAppStoreAuthCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected apple appstore auth to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-apple-appstore-auth" {
		t.Fatalf("expected delegated Rust apple appstore auth output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "apple\nappstore\nauth\nstatus\n--verify=false\n--json" {
		t.Fatalf("expected Rust CLI args to be apple appstore auth + args, got %q", string(argsData))
	}
}

func TestRunAppleAppStoreContextCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runAppleAppStoreContextCommand([]string{"list", "--json"})
	if err != nil {
		t.Fatalf("runAppleAppStoreContextCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go apple appstore context path by default")
	}
}

func TestRunAppleAppStoreContextCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-apple-appstore-context'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runAppleAppStoreContextCommand([]string{"list", "--json"})
		if err != nil {
			t.Fatalf("runAppleAppStoreContextCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected apple appstore context to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-apple-appstore-context" {
		t.Fatalf("expected delegated Rust apple appstore context output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "apple\nappstore\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be apple appstore context + args, got %q", string(argsData))
	}
}

func TestRunAppleAppStoreCommandDefaultsToGoWhileVerifyIsEnabled(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runAppleAppStoreCommand([]string{"auth", "status", "--json"})
	if err != nil {
		t.Fatalf("runAppleAppStoreCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go apple appstore root path while verify is enabled")
	}
}

func TestRunAppleAppStoreCommandDelegatesToRustCLIWhenVerifyDisabled(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-apple-appstore-root'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runAppleAppStoreCommand([]string{"auth", "status", "--verify=false", "--json"})
		if err != nil {
			t.Fatalf("runAppleAppStoreCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected apple appstore root to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-apple-appstore-root" {
		t.Fatalf("expected delegated Rust apple appstore root output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "apple\nappstore\nauth\nstatus\n--verify=false\n--json" {
		t.Fatalf("expected Rust CLI args to be apple appstore root + args, got %q", string(argsData))
	}
}

func TestRunAppleCommandDefaultsToGoForNonMigratedSubtree(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runAppleCommand([]string{"music", "search"})
	if err != nil {
		t.Fatalf("runAppleCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go apple root path for non-migrated subtree")
	}
}

func TestRunAppleCommandDelegatesToRustCLIForMigratedReadPath(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-apple-root'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runAppleCommand([]string{"appstore", "auth", "status", "--verify=false", "--json"})
		if err != nil {
			t.Fatalf("runAppleCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected apple root to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-apple-root" {
		t.Fatalf("expected delegated Rust apple root output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "apple\nappstore\nauth\nstatus\n--verify=false\n--json" {
		t.Fatalf("expected Rust CLI args to be apple root + args, got %q", string(argsData))
	}
}

func TestRunAWSContextListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runAWSContextListCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runAWSContextListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go aws context list path by default")
	}
}

func TestRunAWSContextListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-aws-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runAWSContextListCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runAWSContextListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected aws context list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-aws-list" {
		t.Fatalf("expected delegated Rust aws context list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "aws\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be aws context list + flags, got %q", string(argsData))
	}
}

func TestRunAWSContextCurrentCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runAWSContextCurrentCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runAWSContextCurrentCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go aws context current path by default")
	}
}

func TestRunAWSContextCurrentCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-aws-current'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runAWSContextCurrentCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runAWSContextCurrentCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected aws context current to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-aws-current" {
		t.Fatalf("expected delegated Rust aws context current output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "aws\ncontext\ncurrent\n--json" {
		t.Fatalf("expected Rust CLI args to be aws context current + flags, got %q", string(argsData))
	}
}

func TestRunAWSAuthCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runAWSAuthCommand([]string{"status", "--json"})
	if err != nil {
		t.Fatalf("runAWSAuthCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go aws auth path by default")
	}
}

func TestRunAWSAuthCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-aws-auth-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runAWSAuthCommand([]string{"status", "--json"})
		if err != nil {
			t.Fatalf("runAWSAuthCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected aws auth to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-aws-auth-wrapper" {
		t.Fatalf("expected delegated Rust aws auth output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "aws\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be aws auth + args, got %q", string(argsData))
	}
}

func TestRunAWSContextCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runAWSContextCommand([]string{"list", "--json"})
	if err != nil {
		t.Fatalf("runAWSContextCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go aws context path by default")
	}
}

func TestRunAWSContextCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-aws-context-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runAWSContextCommand([]string{"list", "--json"})
		if err != nil {
			t.Fatalf("runAWSContextCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected aws context to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-aws-context-wrapper" {
		t.Fatalf("expected delegated Rust aws context output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "aws\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be aws context + args, got %q", string(argsData))
	}
}

func TestRunAWSCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runAWSCommand([]string{"auth", "status", "--json"})
	if err != nil {
		t.Fatalf("runAWSCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go aws root path by default")
	}
}

func TestRunAWSCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-aws-root'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runAWSCommand([]string{"auth", "status", "--json"})
		if err != nil {
			t.Fatalf("runAWSCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected aws root to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-aws-root" {
		t.Fatalf("expected delegated Rust aws root output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "aws\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be aws root + args, got %q", string(argsData))
	}
}

func TestRunAWSAuthStatusCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runAWSAuthStatusCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runAWSAuthStatusCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go aws auth status path by default")
	}
}

func TestRunAWSAuthStatusCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-aws-auth'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runAWSAuthStatusCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runAWSAuthStatusCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected aws auth status to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-aws-auth" {
		t.Fatalf("expected delegated Rust aws auth status output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "aws\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be aws auth status + flags, got %q", string(argsData))
	}
}

func TestRunGCPContextListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGCPContextListCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runGCPContextListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go gcp context list path by default")
	}
}

func TestRunGCPContextListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-gcp-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGCPContextListCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runGCPContextListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected gcp context list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-gcp-list" {
		t.Fatalf("expected delegated Rust gcp context list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "gcp\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be gcp context list + flags, got %q", string(argsData))
	}
}

func TestRunGCPContextCurrentCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGCPContextCurrentCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runGCPContextCurrentCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go gcp context current path by default")
	}
}

func TestRunGCPContextCurrentCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-gcp-current'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGCPContextCurrentCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runGCPContextCurrentCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected gcp context current to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-gcp-current" {
		t.Fatalf("expected delegated Rust gcp context current output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "gcp\ncontext\ncurrent\n--json" {
		t.Fatalf("expected Rust CLI args to be gcp context current + flags, got %q", string(argsData))
	}
}

func TestRunGCPAuthStatusCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGCPAuthStatusCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runGCPAuthStatusCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go gcp auth status path by default")
	}
}

func TestRunGCPAuthStatusCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-gcp-auth'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGCPAuthStatusCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runGCPAuthStatusCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected gcp auth status to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-gcp-auth" {
		t.Fatalf("expected delegated Rust gcp auth status output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "gcp\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be gcp auth status + flags, got %q", string(argsData))
	}
}

func TestRunGCPAuthCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGCPAuthCommand([]string{"status", "--json"})
	if err != nil {
		t.Fatalf("runGCPAuthCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go gcp auth path by default")
	}
}

func TestRunGCPAuthCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-gcp-auth-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGCPAuthCommand([]string{"status", "--json"})
		if err != nil {
			t.Fatalf("runGCPAuthCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected gcp auth to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-gcp-auth-wrapper" {
		t.Fatalf("expected delegated Rust gcp auth output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "gcp\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be gcp auth + args, got %q", string(argsData))
	}
}

func TestRunGCPContextCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGCPContextCommand([]string{"list", "--json"})
	if err != nil {
		t.Fatalf("runGCPContextCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go gcp context path by default")
	}
}

func TestRunGCPContextCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-gcp-context-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGCPContextCommand([]string{"list", "--json"})
		if err != nil {
			t.Fatalf("runGCPContextCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected gcp context to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-gcp-context-wrapper" {
		t.Fatalf("expected delegated Rust gcp context output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "gcp\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be gcp context + args, got %q", string(argsData))
	}
}

func TestRunGCPCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGCPCommand([]string{"auth", "status", "--json"})
	if err != nil {
		t.Fatalf("runGCPCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go gcp root path by default")
	}
}

func TestRunGCPCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-gcp-root'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGCPCommand([]string{"auth", "status", "--json"})
		if err != nil {
			t.Fatalf("runGCPCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected gcp root to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-gcp-root" {
		t.Fatalf("expected delegated Rust gcp root output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "gcp\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be gcp root + args, got %q", string(argsData))
	}
}

func TestRunGooglePlacesContextListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGooglePlacesContextListCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runGooglePlacesContextListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go google places context list path by default")
	}
}

func TestRunGooglePlacesContextListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-google-places-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGooglePlacesContextListCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runGooglePlacesContextListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected google places context list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-google-places-list" {
		t.Fatalf("expected delegated Rust google places context list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "google\nplaces\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be google places context list + flags, got %q", string(argsData))
	}
}

func TestRunGooglePlacesContextCurrentCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGooglePlacesContextCurrentCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runGooglePlacesContextCurrentCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go google places context current path by default")
	}
}

func TestRunGooglePlacesContextCurrentCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-google-places-current'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGooglePlacesContextCurrentCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runGooglePlacesContextCurrentCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected google places context current to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-google-places-current" {
		t.Fatalf("expected delegated Rust google places context current output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "google\nplaces\ncontext\ncurrent\n--json" {
		t.Fatalf("expected Rust CLI args to be google places context current + flags, got %q", string(argsData))
	}
}

func TestRunGooglePlacesAuthCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGooglePlacesAuthCommand([]string{"status", "--json"})
	if err != nil {
		t.Fatalf("runGooglePlacesAuthCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go google places auth path by default")
	}
}

func TestRunGooglePlacesAuthCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-google-places-auth-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGooglePlacesAuthCommand([]string{"status", "--json"})
		if err != nil {
			t.Fatalf("runGooglePlacesAuthCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected google places auth to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-google-places-auth-wrapper" {
		t.Fatalf("expected delegated Rust google places auth output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "google\nplaces\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be google places auth + args, got %q", string(argsData))
	}
}

func TestRunGooglePlacesContextCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGooglePlacesContextCommand([]string{"list", "--json"})
	if err != nil {
		t.Fatalf("runGooglePlacesContextCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go google places context path by default")
	}
}

func TestRunGooglePlacesContextCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-google-places-context-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGooglePlacesContextCommand([]string{"list", "--json"})
		if err != nil {
			t.Fatalf("runGooglePlacesContextCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected google places context to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-google-places-context-wrapper" {
		t.Fatalf("expected delegated Rust google places context output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "google\nplaces\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be google places context + args, got %q", string(argsData))
	}
}

func TestRunGooglePlacesCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGooglePlacesCommand([]string{"auth", "status", "--json"})
	if err != nil {
		t.Fatalf("runGooglePlacesCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go google places root path by default")
	}
}

func TestRunGooglePlacesCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-google-places-root'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGooglePlacesCommand([]string{"auth", "status", "--json"})
		if err != nil {
			t.Fatalf("runGooglePlacesCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected google places root to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-google-places-root" {
		t.Fatalf("expected delegated Rust google places root output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "google\nplaces\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be google places root + args, got %q", string(argsData))
	}
}

func TestRunGoogleCommandDefaultsToGoForNonMigratedSubtree(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGoogleCommand([]string{"youtube", "search"})
	if err != nil {
		t.Fatalf("runGoogleCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go google root path for non-migrated subtree")
	}
}

func TestRunGoogleCommandDelegatesToRustCLIForMigratedReadPath(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-google-root'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGoogleCommand([]string{"places", "context", "list", "--json"})
		if err != nil {
			t.Fatalf("runGoogleCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected google root to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-google-root" {
		t.Fatalf("expected delegated Rust google root output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "google\nplaces\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be google root + args, got %q", string(argsData))
	}
}

func TestRunOpenAICommandDefaultsToGoForMutationPath(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAICommand([]string{"project", "create", "--json"})
	if err != nil {
		t.Fatalf("runOpenAICommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai root path for mutation subtree")
	}
}

func TestRunOpenAICommandDelegatesToRustCLIForMigratedReadPath(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-root'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAICommand([]string{"project", "api-key", "list", "--project-id", "proj_123", "--json"})
		if err != nil {
			t.Fatalf("runOpenAICommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai root to delegate migrated read subtree to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-root" {
		t.Fatalf("expected delegated Rust openai root output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nproject\napi-key\nlist\n--project-id\nproj_123\n--json" {
		t.Fatalf("expected Rust CLI args to be openai root + args, got %q", string(argsData))
	}
}

func TestRunOpenAIAuthCommandDefaultsToGoForCodexStatus(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIAuthCommand([]string{"codex-status", "--json"})
	if err != nil {
		t.Fatalf("runOpenAIAuthCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai auth path for codex-status")
	}
}

func TestRunOpenAIAuthCommandDelegatesToRustCLIForAPIStatus(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-auth-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIAuthCommand([]string{"status", "--json"})
		if err != nil {
			t.Fatalf("runOpenAIAuthCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai auth to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-auth-wrapper" {
		t.Fatalf("expected delegated Rust openai auth output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be openai auth + args, got %q", string(argsData))
	}
}

func TestRunOpenAIProjectCommandDefaultsToGoForMutationPath(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIProjectCommand([]string{"create", "--json"})
	if err != nil {
		t.Fatalf("runOpenAIProjectCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai project path for create")
	}
}

func TestRunOpenAIProjectCommandDelegatesToRustCLIForList(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-project-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIProjectCommand([]string{"list", "--json"})
		if err != nil {
			t.Fatalf("runOpenAIProjectCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai project to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-project-wrapper" {
		t.Fatalf("expected delegated Rust openai project output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nproject\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be openai project + args, got %q", string(argsData))
	}
}

func TestRunOpenAIProjectRateLimitCommandDefaultsToGoForUpdate(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIProjectRateLimitCommand([]string{"update", "--json"})
	if err != nil {
		t.Fatalf("runOpenAIProjectRateLimitCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai project rate-limit path for update")
	}
}

func TestRunOpenAIProjectRateLimitCommandDelegatesToRustCLIForList(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-project-rate-limit-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIProjectRateLimitCommand([]string{"list", "--project-id", "proj_123", "--json"})
		if err != nil {
			t.Fatalf("runOpenAIProjectRateLimitCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai project rate-limit to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-project-rate-limit-wrapper" {
		t.Fatalf("expected delegated Rust openai project rate-limit output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nproject\nrate-limit\nlist\n--project-id\nproj_123\n--json" {
		t.Fatalf("expected Rust CLI args to be openai project rate-limit + args, got %q", string(argsData))
	}
}

func TestRunOpenAIProjectAPIKeyCommandDefaultsToGoForDelete(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIProjectAPIKeyCommand([]string{"delete", "--json"})
	if err != nil {
		t.Fatalf("runOpenAIProjectAPIKeyCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai project api-key path for delete")
	}
}

func TestRunOpenAIProjectAPIKeyCommandDelegatesToRustCLIForList(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-project-api-key-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIProjectAPIKeyCommand([]string{"list", "--project-id", "proj_123", "--json"})
		if err != nil {
			t.Fatalf("runOpenAIProjectAPIKeyCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai project api-key to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-project-api-key-wrapper" {
		t.Fatalf("expected delegated Rust openai project api-key output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nproject\napi-key\nlist\n--project-id\nproj_123\n--json" {
		t.Fatalf("expected Rust CLI args to be openai project api-key + args, got %q", string(argsData))
	}
}

func TestRunOpenAIProjectServiceAccountCommandDefaultsToGoForCreate(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIProjectServiceAccountCommand([]string{"create", "--json"})
	if err != nil {
		t.Fatalf("runOpenAIProjectServiceAccountCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai project service-account path for create")
	}
}

func TestRunOpenAIProjectServiceAccountCommandDelegatesToRustCLIForList(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-project-service-account-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIProjectServiceAccountCommand([]string{"list", "--project-id", "proj_123", "--json"})
		if err != nil {
			t.Fatalf("runOpenAIProjectServiceAccountCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai project service-account to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-project-service-account-wrapper" {
		t.Fatalf("expected delegated Rust openai project service-account output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nproject\nservice-account\nlist\n--project-id\nproj_123\n--json" {
		t.Fatalf("expected Rust CLI args to be openai project service-account + args, got %q", string(argsData))
	}
}

func TestRunOpenAIKeyCommandDefaultsToGoForCreate(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIKeyCommand([]string{"create", "--json"})
	if err != nil {
		t.Fatalf("runOpenAIKeyCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai key path for create")
	}
}

func TestRunOpenAIKeyCommandDelegatesToRustCLIForList(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-key-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIKeyCommand([]string{"list", "--json"})
		if err != nil {
			t.Fatalf("runOpenAIKeyCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai key to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-key-wrapper" {
		t.Fatalf("expected delegated Rust openai key output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nkey\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be openai key + args, got %q", string(argsData))
	}
}

func TestRunGooglePlacesAuthStatusCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGooglePlacesAuthStatusCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runGooglePlacesAuthStatusCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go google places auth status path by default")
	}
}

func TestRunGooglePlacesAuthStatusCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-google-places-auth'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGooglePlacesAuthStatusCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runGooglePlacesAuthStatusCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected google places auth status to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-google-places-auth" {
		t.Fatalf("expected delegated Rust google places auth status output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "google\nplaces\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be google places auth status + flags, got %q", string(argsData))
	}
}

func TestRunOpenAIContextListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIContextListCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runOpenAIContextListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai context list path by default")
	}
}

func TestRunOpenAIContextListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIContextListCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runOpenAIContextListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai context list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-list" {
		t.Fatalf("expected delegated Rust openai context list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be openai context list + flags, got %q", string(argsData))
	}
}

func TestRunOpenAIContextCurrentCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIContextCurrentCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runOpenAIContextCurrentCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai context current path by default")
	}
}

func TestRunOpenAIContextCurrentCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-current'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIContextCurrentCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runOpenAIContextCurrentCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai context current to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-current" {
		t.Fatalf("expected delegated Rust openai context current output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\ncontext\ncurrent\n--json" {
		t.Fatalf("expected Rust CLI args to be openai context current + flags, got %q", string(argsData))
	}
}

func TestRunOpenAIContextCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIContextCommand([]string{"list", "--json"})
	if err != nil {
		t.Fatalf("runOpenAIContextCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai context path by default")
	}
}

func TestRunOpenAIContextCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-context-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIContextCommand([]string{"list", "--json"})
		if err != nil {
			t.Fatalf("runOpenAIContextCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai context to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-context-wrapper" {
		t.Fatalf("expected delegated Rust openai context output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be openai context + args, got %q", string(argsData))
	}
}

func TestRunOpenAIAuthStatusCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIAuthStatusCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runOpenAIAuthStatusCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai auth status path by default")
	}
}

func TestRunOpenAIAuthStatusCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-auth-status'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIAuthStatusCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runOpenAIAuthStatusCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai auth status to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-auth-status" {
		t.Fatalf("expected delegated Rust openai auth status output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be openai auth status + flags, got %q", string(argsData))
	}
}

func TestRunOpenAIModelCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIModelCommand([]string{"list", "--json", "--limit", "1"})
	if err != nil {
		t.Fatalf("runOpenAIModelCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai model path by default")
	}
}

func TestRunOpenAIModelCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-model'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIModelCommand([]string{"list", "--json", "--limit", "1"})
		if err != nil {
			t.Fatalf("runOpenAIModelCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai model to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-model" {
		t.Fatalf("expected delegated Rust openai model output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nmodel\nlist\n--json\n--limit\n1" {
		t.Fatalf("expected Rust CLI args to be openai model + args, got %q", string(argsData))
	}
}

func TestRunOpenAIModelListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIModelListCommand([]string{"--json", "--limit", "1"})
	if err != nil {
		t.Fatalf("runOpenAIModelListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai model list path by default")
	}
}

func TestRunOpenAIModelListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-model-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIModelListCommand([]string{"--json", "--limit", "1"})
		if err != nil {
			t.Fatalf("runOpenAIModelListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai model list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-model-list" {
		t.Fatalf("expected delegated Rust openai model list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nmodel\nlist\n--json\n--limit\n1" {
		t.Fatalf("expected Rust CLI args to be openai model list + flags, got %q", string(argsData))
	}
}

func TestRunOpenAIModelGetCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIModelGetCommand([]string{"gpt-test"})
	if err != nil {
		t.Fatalf("runOpenAIModelGetCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai model get path by default")
	}
}

func TestRunOpenAIModelGetCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-model-get'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIModelGetCommand([]string{"gpt-test"})
		if err != nil {
			t.Fatalf("runOpenAIModelGetCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai model get to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-model-get" {
		t.Fatalf("expected delegated Rust openai model get output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nmodel\nget\ngpt-test" {
		t.Fatalf("expected Rust CLI args to be openai model get + arg, got %q", string(argsData))
	}
}

func TestRunOpenAIUsageCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIUsageCommand([]string{"completions", "--json", "--limit", "1"})
	if err != nil {
		t.Fatalf("runOpenAIUsageCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai usage path by default")
	}
}

func TestRunOpenAIUsageCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-usage'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIUsageCommand([]string{"completions", "--json", "--limit", "1"})
		if err != nil {
			t.Fatalf("runOpenAIUsageCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai usage to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-usage" {
		t.Fatalf("expected delegated Rust openai usage output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nusage\ncompletions\n--json\n--limit\n1" {
		t.Fatalf("expected Rust CLI args to be openai usage + args, got %q", string(argsData))
	}
}

func TestRunOpenAIUsageMetricCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIUsageMetricCommand("completions", []string{"--json", "--limit", "1"})
	if err != nil {
		t.Fatalf("runOpenAIUsageMetricCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai usage path by default")
	}
}

func TestRunOpenAIUsageMetricCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-usage'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIUsageMetricCommand("completions", []string{"--json", "--limit", "1"})
		if err != nil {
			t.Fatalf("runOpenAIUsageMetricCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai usage to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-usage" {
		t.Fatalf("expected delegated Rust openai usage output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nusage\ncompletions\n--json\n--limit\n1" {
		t.Fatalf("expected Rust CLI args to be openai usage completions + flags, got %q", string(argsData))
	}
}

func TestRunOpenAICodexUsageCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAICodexUsageCommand([]string{"--json", "--limit", "1"})
	if err != nil {
		t.Fatalf("runOpenAICodexUsageCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai codex usage path by default")
	}
}

func TestRunOpenAICodexUsageCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-codex-usage'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAICodexUsageCommand([]string{"--json", "--limit", "1"})
		if err != nil {
			t.Fatalf("runOpenAICodexUsageCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai codex usage to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-codex-usage" {
		t.Fatalf("expected delegated Rust openai codex usage output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\ncodex\nusage\n--json\n--limit\n1" {
		t.Fatalf("expected Rust CLI args to be openai codex usage + flags, got %q", string(argsData))
	}
}

func TestRunOpenAIMonitorCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIMonitorCommand([]string{"usage", "--json", "--limit", "1"})
	if err != nil {
		t.Fatalf("runOpenAIMonitorCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai monitor path by default")
	}
}

func TestRunOpenAIMonitorCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-monitor'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIMonitorCommand([]string{"usage", "--json", "--limit", "1"})
		if err != nil {
			t.Fatalf("runOpenAIMonitorCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai monitor to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-monitor" {
		t.Fatalf("expected delegated Rust openai monitor output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nmonitor\nusage\n--json\n--limit\n1" {
		t.Fatalf("expected Rust CLI args to be openai monitor + args, got %q", string(argsData))
	}
}

func TestRunOpenAICodexCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAICodexCommand([]string{"usage", "--json", "--limit", "1"})
	if err != nil {
		t.Fatalf("runOpenAICodexCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai codex path by default")
	}
}

func TestRunOpenAICodexCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-codex'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAICodexCommand([]string{"usage", "--json", "--limit", "1"})
		if err != nil {
			t.Fatalf("runOpenAICodexCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai codex to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-codex" {
		t.Fatalf("expected delegated Rust openai codex output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\ncodex\nusage\n--json\n--limit\n1" {
		t.Fatalf("expected Rust CLI args to be openai codex + args, got %q", string(argsData))
	}
}

func TestRunOpenAIKeyListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIKeyListCommand([]string{"--json", "--limit", "1"})
	if err != nil {
		t.Fatalf("runOpenAIKeyListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai key list path by default")
	}
}

func TestRunOpenAIKeyListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-key-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIKeyListCommand([]string{"--json", "--limit", "1"})
		if err != nil {
			t.Fatalf("runOpenAIKeyListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai key list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-key-list" {
		t.Fatalf("expected delegated Rust openai key list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nkey\nlist\n--json\n--limit\n1" {
		t.Fatalf("expected Rust CLI args to be openai key list + flags, got %q", string(argsData))
	}
}

func TestRunOpenAIKeyGetCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIKeyGetCommand([]string{"key_123"})
	if err != nil {
		t.Fatalf("runOpenAIKeyGetCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai key get path by default")
	}
}

func TestRunOpenAIKeyGetCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-key-get'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIKeyGetCommand([]string{"key_123"})
		if err != nil {
			t.Fatalf("runOpenAIKeyGetCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai key get to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-key-get" {
		t.Fatalf("expected delegated Rust openai key get output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nkey\nget\nkey_123" {
		t.Fatalf("expected Rust CLI args to be openai key get + arg, got %q", string(argsData))
	}
}

func TestRunOpenAIProjectListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIProjectListCommand([]string{"--json", "--limit", "1"})
	if err != nil {
		t.Fatalf("runOpenAIProjectListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai project list path by default")
	}
}

func TestRunOpenAIProjectListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-project-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIProjectListCommand([]string{"--json", "--limit", "1"})
		if err != nil {
			t.Fatalf("runOpenAIProjectListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai project list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-project-list" {
		t.Fatalf("expected delegated Rust openai project list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nproject\nlist\n--json\n--limit\n1" {
		t.Fatalf("expected Rust CLI args to be openai project list + flags, got %q", string(argsData))
	}
}

func TestRunOpenAIProjectGetCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIProjectGetCommand([]string{"proj_123"})
	if err != nil {
		t.Fatalf("runOpenAIProjectGetCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai project get path by default")
	}
}

func TestRunOpenAIProjectGetCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-project-get'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIProjectGetCommand([]string{"proj_123"})
		if err != nil {
			t.Fatalf("runOpenAIProjectGetCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai project get to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-project-get" {
		t.Fatalf("expected delegated Rust openai project get output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nproject\nget\nproj_123" {
		t.Fatalf("expected Rust CLI args to be openai project get + arg, got %q", string(argsData))
	}
}

func TestRunOpenAIProjectAPIKeyListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIProjectAPIKeyListCommand([]string{"--json", "--project-id", "proj_123"})
	if err != nil {
		t.Fatalf("runOpenAIProjectAPIKeyListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai project api-key list path by default")
	}
}

func TestRunOpenAIProjectAPIKeyListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-project-api-key-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIProjectAPIKeyListCommand([]string{"--json", "--project-id", "proj_123"})
		if err != nil {
			t.Fatalf("runOpenAIProjectAPIKeyListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai project api-key list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-project-api-key-list" {
		t.Fatalf("expected delegated Rust openai project api-key list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nproject\napi-key\nlist\n--json\n--project-id\nproj_123" {
		t.Fatalf("expected Rust CLI args to be openai project api-key list + flags, got %q", string(argsData))
	}
}

func TestRunOpenAIProjectAPIKeyGetCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIProjectAPIKeyGetCommand([]string{"--project-id", "proj_123", "key_123"})
	if err != nil {
		t.Fatalf("runOpenAIProjectAPIKeyGetCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai project api-key get path by default")
	}
}

func TestRunOpenAIProjectAPIKeyGetCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-project-api-key-get'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIProjectAPIKeyGetCommand([]string{"--project-id", "proj_123", "key_123"})
		if err != nil {
			t.Fatalf("runOpenAIProjectAPIKeyGetCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai project api-key get to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-project-api-key-get" {
		t.Fatalf("expected delegated Rust openai project api-key get output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nproject\napi-key\nget\n--project-id\nproj_123\nkey_123" {
		t.Fatalf("expected Rust CLI args to be openai project api-key get + args, got %q", string(argsData))
	}
}

func TestRunOpenAIProjectServiceAccountListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIProjectServiceAccountListCommand([]string{"--json", "--project-id", "proj_123"})
	if err != nil {
		t.Fatalf("runOpenAIProjectServiceAccountListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai project service-account list path by default")
	}
}

func TestRunOpenAIProjectServiceAccountListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-project-service-account-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIProjectServiceAccountListCommand([]string{"--json", "--project-id", "proj_123"})
		if err != nil {
			t.Fatalf("runOpenAIProjectServiceAccountListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai project service-account list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-project-service-account-list" {
		t.Fatalf("expected delegated Rust openai project service-account list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nproject\nservice-account\nlist\n--json\n--project-id\nproj_123" {
		t.Fatalf("expected Rust CLI args to be openai project service-account list + flags, got %q", string(argsData))
	}
}

func TestRunOpenAIProjectServiceAccountGetCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIProjectServiceAccountGetCommand([]string{"--project-id", "proj_123", "sa_123"})
	if err != nil {
		t.Fatalf("runOpenAIProjectServiceAccountGetCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai project service-account get path by default")
	}
}

func TestRunOpenAIProjectServiceAccountGetCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-project-service-account-get'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIProjectServiceAccountGetCommand([]string{"--project-id", "proj_123", "sa_123"})
		if err != nil {
			t.Fatalf("runOpenAIProjectServiceAccountGetCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai project service-account get to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-project-service-account-get" {
		t.Fatalf("expected delegated Rust openai project service-account get output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nproject\nservice-account\nget\n--project-id\nproj_123\nsa_123" {
		t.Fatalf("expected Rust CLI args to be openai project service-account get + args, got %q", string(argsData))
	}
}

func TestRunOpenAIProjectRateLimitListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOpenAIProjectRateLimitListCommand([]string{"--json", "--project-id", "proj_123"})
	if err != nil {
		t.Fatalf("runOpenAIProjectRateLimitListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go openai project rate-limit list path by default")
	}
}

func TestRunOpenAIProjectRateLimitListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-openai-project-rate-limit-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOpenAIProjectRateLimitListCommand([]string{"--json", "--project-id", "proj_123"})
		if err != nil {
			t.Fatalf("runOpenAIProjectRateLimitListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected openai project rate-limit list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-openai-project-rate-limit-list" {
		t.Fatalf("expected delegated Rust openai project rate-limit list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "openai\nproject\nrate-limit\nlist\n--json\n--project-id\nproj_123" {
		t.Fatalf("expected Rust CLI args to be openai project rate-limit list + flags, got %q", string(argsData))
	}
}

func TestRunOCIContextListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOCIContextListCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runOCIContextListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go oci context list path by default")
	}
}

func TestRunOCIContextListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-oci-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOCIContextListCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runOCIContextListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected oci context list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-oci-list" {
		t.Fatalf("expected delegated Rust oci context list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "oci\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be oci context list + flags, got %q", string(argsData))
	}
}

func TestRunOCIContextCurrentCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOCIContextCurrentCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runOCIContextCurrentCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go oci context current path by default")
	}
}

func TestRunOCIContextCurrentCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-oci-current'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOCIContextCurrentCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runOCIContextCurrentCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected oci context current to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-oci-current" {
		t.Fatalf("expected delegated Rust oci context current output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "oci\ncontext\ncurrent\n--json" {
		t.Fatalf("expected Rust CLI args to be oci context current + flags, got %q", string(argsData))
	}
}

func TestRunOCIAuthStatusCommandDefaultsToGoWhileVerifyIsEnabled(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOCIAuthStatusCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runOCIAuthStatusCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go oci auth status path while verification stays on the Go side")
	}
}

func TestRunOCIAuthStatusCommandDelegatesToRustCLIWhenVerifyDisabled(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-oci-auth'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOCIAuthStatusCommand([]string{"--verify=false", "--json"})
		if err != nil {
			t.Fatalf("runOCIAuthStatusCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected oci auth status to delegate to Rust when verification is disabled")
		}
	})

	if strings.TrimSpace(out) != "rust-oci-auth" {
		t.Fatalf("expected delegated Rust oci auth status output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "oci\nauth\nstatus\n--verify=false\n--json" {
		t.Fatalf("expected Rust CLI args to be oci auth status + flags, got %q", string(argsData))
	}
}

func TestRunOCIAuthCommandDefaultsToGoWhileVerifyIsEnabled(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOCIAuthCommand([]string{"status", "--json"})
	if err != nil {
		t.Fatalf("runOCIAuthCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go oci auth path while verify is enabled")
	}
}

func TestRunOCIAuthCommandDelegatesToRustCLIWhenVerifyDisabled(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-oci-auth-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOCIAuthCommand([]string{"status", "--verify=false", "--json"})
		if err != nil {
			t.Fatalf("runOCIAuthCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected oci auth to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-oci-auth-wrapper" {
		t.Fatalf("expected delegated Rust oci auth output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "oci\nauth\nstatus\n--verify=false\n--json" {
		t.Fatalf("expected Rust CLI args to be oci auth + args, got %q", string(argsData))
	}
}

func TestRunOCIContextCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOCIContextCommand([]string{"list", "--json"})
	if err != nil {
		t.Fatalf("runOCIContextCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go oci context path by default")
	}
}

func TestRunOCIContextCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-oci-context-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOCIContextCommand([]string{"list", "--json"})
		if err != nil {
			t.Fatalf("runOCIContextCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected oci context to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-oci-context-wrapper" {
		t.Fatalf("expected delegated Rust oci context output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "oci\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be oci context + args, got %q", string(argsData))
	}
}

func TestRunOCICommandDefaultsToGoForUnmigratedSubtree(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOCICommand([]string{"identity", "compartment", "list", "--json"})
	if err != nil {
		t.Fatalf("runOCICommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go oci root path for unmigrated subtree")
	}
}

func TestRunOCICommandDelegatesToRustCLIForMigratedReadPath(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-oci-root'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOCICommand([]string{"oracular", "tenancy", "--json"})
		if err != nil {
			t.Fatalf("runOCICommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected oci root to delegate migrated read subtree to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-oci-root" {
		t.Fatalf("expected delegated Rust oci root output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "oci\noracular\ntenancy\n--json" {
		t.Fatalf("expected Rust CLI args to be oci root + args, got %q", string(argsData))
	}
}

func TestRunOCIOracularCommandDefaultsToGoForCloudInit(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOCIOracularCommand([]string{"cloud-init", "--json"})
	if err != nil {
		t.Fatalf("runOCIOracularCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go oci oracular path for cloud-init")
	}
}

func TestRunOCIOracularCommandDelegatesToRustCLIForTenancy(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-oci-oracular-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOCIOracularCommand([]string{"tenancy", "--json"})
		if err != nil {
			t.Fatalf("runOCIOracularCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected oci oracular to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-oci-oracular-wrapper" {
		t.Fatalf("expected delegated Rust oci oracular output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "oci\noracular\ntenancy\n--json" {
		t.Fatalf("expected Rust CLI args to be oci oracular + args, got %q", string(argsData))
	}
}

func TestRunOCIOracularTenancyCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runOCIOracularTenancyCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runOCIOracularTenancyCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go oci oracular tenancy path by default")
	}
}

func TestRunOCIOracularTenancyCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-oci-oracular-tenancy'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runOCIOracularTenancyCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runOCIOracularTenancyCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected oci oracular tenancy to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-oci-oracular-tenancy" {
		t.Fatalf("expected delegated Rust oci oracular tenancy output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "oci\noracular\ntenancy\n--json" {
		t.Fatalf("expected Rust CLI args to be oci oracular tenancy + flags, got %q", string(argsData))
	}
}

func TestRunStripeContextListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runStripeContextListCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runStripeContextListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go stripe context list path by default")
	}
}

func TestRunStripeContextListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-stripe-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runStripeContextListCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runStripeContextListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected stripe context list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-stripe-list" {
		t.Fatalf("expected delegated Rust stripe context list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "stripe\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be stripe context list + flags, got %q", string(argsData))
	}
}

func TestRunStripeContextCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runStripeContextCommand([]string{"list", "--json"})
	if err != nil {
		t.Fatalf("runStripeContextCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go stripe context path by default")
	}
}

func TestRunStripeContextCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-stripe-context-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runStripeContextCommand([]string{"list", "--json"})
		if err != nil {
			t.Fatalf("runStripeContextCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected stripe context to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-stripe-context-wrapper" {
		t.Fatalf("expected delegated Rust stripe context output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "stripe\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be stripe context + args, got %q", string(argsData))
	}
}

func TestRunStripeContextCurrentCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runStripeContextCurrentCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runStripeContextCurrentCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go stripe context current path by default")
	}
}

func TestRunStripeContextCurrentCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-stripe-current'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runStripeContextCurrentCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runStripeContextCurrentCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected stripe context current to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-stripe-current" {
		t.Fatalf("expected delegated Rust stripe context current output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "stripe\ncontext\ncurrent\n--json" {
		t.Fatalf("expected Rust CLI args to be stripe context current + flags, got %q", string(argsData))
	}
}

func TestRunStripeAuthCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runStripeAuthCommand([]string{"status", "--json"})
	if err != nil {
		t.Fatalf("runStripeAuthCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go stripe auth path by default")
	}
}

func TestRunStripeAuthCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-stripe-auth-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runStripeAuthCommand([]string{"status", "--json"})
		if err != nil {
			t.Fatalf("runStripeAuthCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected stripe auth to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-stripe-auth-wrapper" {
		t.Fatalf("expected delegated Rust stripe auth output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "stripe\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be stripe auth + args, got %q", string(argsData))
	}
}

func TestRunStripeAuthStatusCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runStripeAuthStatusCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runStripeAuthStatusCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go stripe auth status path by default")
	}
}

func TestRunStripeAuthStatusCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-stripe-auth'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runStripeAuthStatusCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runStripeAuthStatusCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected stripe auth status to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-stripe-auth" {
		t.Fatalf("expected delegated Rust stripe auth status output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "stripe\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be stripe auth status + flags, got %q", string(argsData))
	}
}

func TestRunStripeCommandDefaultsToGoForNonMigratedSubtree(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runStripeCommand([]string{"object", "list", "--json"})
	if err != nil {
		t.Fatalf("runStripeCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go stripe root path for non-migrated subtree")
	}
}

func TestRunStripeCommandDelegatesToRustCLIForMigratedReadPath(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-stripe-root'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runStripeCommand([]string{"context", "list", "--json"})
		if err != nil {
			t.Fatalf("runStripeCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected stripe root to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-stripe-root" {
		t.Fatalf("expected delegated Rust stripe root output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "stripe\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be stripe root + args, got %q", string(argsData))
	}
}

func TestRunWorkOSContextListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runWorkOSContextListCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runWorkOSContextListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go workos context list path by default")
	}
}

func TestRunWorkOSContextListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-workos-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runWorkOSContextListCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runWorkOSContextListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected workos context list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-workos-list" {
		t.Fatalf("expected delegated Rust workos context list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "workos\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be workos context list + flags, got %q", string(argsData))
	}
}

func TestRunWorkOSContextCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runWorkOSContextCommand([]string{"list", "--json"})
	if err != nil {
		t.Fatalf("runWorkOSContextCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go workos context path by default")
	}
}

func TestRunWorkOSContextCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-workos-context-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runWorkOSContextCommand([]string{"list", "--json"})
		if err != nil {
			t.Fatalf("runWorkOSContextCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected workos context to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-workos-context-wrapper" {
		t.Fatalf("expected delegated Rust workos context output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "workos\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be workos context + args, got %q", string(argsData))
	}
}

func TestRunWorkOSContextCurrentCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runWorkOSContextCurrentCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runWorkOSContextCurrentCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go workos context current path by default")
	}
}

func TestRunWorkOSContextCurrentCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-workos-current'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runWorkOSContextCurrentCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runWorkOSContextCurrentCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected workos context current to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-workos-current" {
		t.Fatalf("expected delegated Rust workos context current output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "workos\ncontext\ncurrent\n--json" {
		t.Fatalf("expected Rust CLI args to be workos context current + flags, got %q", string(argsData))
	}
}

func TestRunWorkOSAuthCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runWorkOSAuthCommand([]string{"status", "--json"})
	if err != nil {
		t.Fatalf("runWorkOSAuthCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go workos auth path by default")
	}
}

func TestRunWorkOSAuthCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-workos-auth-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runWorkOSAuthCommand([]string{"status", "--json"})
		if err != nil {
			t.Fatalf("runWorkOSAuthCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected workos auth to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-workos-auth-wrapper" {
		t.Fatalf("expected delegated Rust workos auth output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "workos\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be workos auth + args, got %q", string(argsData))
	}
}

func TestRunWorkOSAuthStatusCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runWorkOSAuthStatusCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runWorkOSAuthStatusCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go workos auth status path by default")
	}
}

func TestRunWorkOSAuthStatusCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-workos-auth'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runWorkOSAuthStatusCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runWorkOSAuthStatusCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected workos auth status to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-workos-auth" {
		t.Fatalf("expected delegated Rust workos auth status output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "workos\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be workos auth status + flags, got %q", string(argsData))
	}
}

func TestRunWorkOSCommandDefaultsToGoForNonMigratedSubtree(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runWorkOSCommand([]string{"organization", "list", "--json"})
	if err != nil {
		t.Fatalf("runWorkOSCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go workos root path for non-migrated subtree")
	}
}

func TestRunWorkOSCommandDelegatesToRustCLIForMigratedReadPath(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-workos-root'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runWorkOSCommand([]string{"context", "list", "--json"})
		if err != nil {
			t.Fatalf("runWorkOSCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected workos root to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-workos-root" {
		t.Fatalf("expected delegated Rust workos root output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "workos\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be workos root + args, got %q", string(argsData))
	}
}
func TestRunGitHubContextListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubContextListCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runGitHubContextListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github context list path by default")
	}
}

func TestRunGitHubContextListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-contexts'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubContextListCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runGitHubContextListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github context list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-contexts" {
		t.Fatalf("expected delegated Rust output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be github context list + flags, got %q", string(argsData))
	}
}

func TestRunGitHubContextCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubContextCommand([]string{"list", "--json"})
	if err != nil {
		t.Fatalf("runGitHubContextCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github context path by default")
	}
}

func TestRunGitHubContextCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-context-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubContextCommand([]string{"list", "--json"})
		if err != nil {
			t.Fatalf("runGitHubContextCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github context to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-context-wrapper" {
		t.Fatalf("expected delegated Rust github context output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\ncontext\nlist\n--json" {
		t.Fatalf("expected Rust CLI args to be github context + args, got %q", string(argsData))
	}
}

func TestRunGitHubContextCurrentCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubContextCurrentCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runGitHubContextCurrentCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github context current path by default")
	}
}

func TestRunGitHubContextCurrentCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-current'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubContextCurrentCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runGitHubContextCurrentCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github context current to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-current" {
		t.Fatalf("expected delegated Rust output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\ncontext\ncurrent\n--json" {
		t.Fatalf("expected Rust CLI args to be github context current + flags, got %q", string(argsData))
	}
}

func TestRunGitHubAuthCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubAuthCommand([]string{"status", "--json"})
	if err != nil {
		t.Fatalf("runGitHubAuthCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github auth path by default")
	}
}

func TestRunGitHubAuthCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-auth-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubAuthCommand([]string{"status", "--json"})
		if err != nil {
			t.Fatalf("runGitHubAuthCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github auth to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-auth-wrapper" {
		t.Fatalf("expected delegated Rust github auth output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be github auth + args, got %q", string(argsData))
	}
}

func TestRunGitHubAuthStatusCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubAuthStatusCommand([]string{"--json"})
	if err != nil {
		t.Fatalf("runGitHubAuthStatusCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github auth status path by default")
	}
}

func TestRunGitHubAuthStatusCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-auth-status'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubAuthStatusCommand([]string{"--json"})
		if err != nil {
			t.Fatalf("runGitHubAuthStatusCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github auth status to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-auth-status" {
		t.Fatalf("expected delegated Rust github auth status output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nauth\nstatus\n--json" {
		t.Fatalf("expected Rust CLI args to be github auth status + flags, got %q", string(argsData))
	}
}

func TestRunGitHubReleaseCommandDefaultsToGoForMutationPath(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubReleaseCommand([]string{"create", "Aureuma/si", "--json"})
	if err != nil {
		t.Fatalf("runGitHubReleaseCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github release mutation path by default")
	}
}

func TestRunGitHubReleaseCommandDelegatesToRustCLIForReadPath(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-release-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubReleaseCommand([]string{"list", "Aureuma/si", "--json"})
		if err != nil {
			t.Fatalf("runGitHubReleaseCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github release wrapper to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-release-wrapper" {
		t.Fatalf("expected delegated Rust github release output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nrelease\nlist\nAureuma/si\n--json" {
		t.Fatalf("expected Rust CLI args to be github release + args, got %q", string(argsData))
	}
}

func TestRunGitHubReleaseListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubReleaseListCommand([]string{"Aureuma/si", "--json"})
	if err != nil {
		t.Fatalf("runGitHubReleaseListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github release list path by default")
	}
}

func TestRunGitHubReleaseListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-release-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubReleaseListCommand([]string{"Aureuma/si", "--json"})
		if err != nil {
			t.Fatalf("runGitHubReleaseListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github release list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-release-list" {
		t.Fatalf("expected delegated Rust github release list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nrelease\nlist\nAureuma/si\n--json" {
		t.Fatalf("expected Rust CLI args to be github release list + args, got %q", string(argsData))
	}
}

func TestRunGitHubReleaseGetCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubReleaseGetCommand([]string{"Aureuma/si", "v1.2.3", "--json"})
	if err != nil {
		t.Fatalf("runGitHubReleaseGetCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github release get path by default")
	}
}

func TestRunGitHubReleaseGetCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-release-get'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubReleaseGetCommand([]string{"Aureuma/si", "v1.2.3", "--json"})
		if err != nil {
			t.Fatalf("runGitHubReleaseGetCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github release get to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-release-get" {
		t.Fatalf("expected delegated Rust github release get output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nrelease\nget\nAureuma/si\nv1.2.3\n--json" {
		t.Fatalf("expected Rust CLI args to be github release get + args, got %q", string(argsData))
	}
}

func TestRunGitHubRepoCommandDefaultsToGoForMutationPath(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubRepoCommand([]string{"create", "si", "--json"})
	if err != nil {
		t.Fatalf("runGitHubRepoCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github repo mutation path by default")
	}
}

func TestRunGitHubRepoCommandDelegatesToRustCLIForReadPath(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-repo-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubRepoCommand([]string{"list", "Aureuma", "--json"})
		if err != nil {
			t.Fatalf("runGitHubRepoCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github repo wrapper to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-repo-wrapper" {
		t.Fatalf("expected delegated Rust github repo output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nrepo\nlist\nAureuma\n--json" {
		t.Fatalf("expected Rust CLI args to be github repo + args, got %q", string(argsData))
	}
}

func TestRunGitHubRepoListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubRepoListCommand([]string{"Aureuma", "--json"})
	if err != nil {
		t.Fatalf("runGitHubRepoListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github repo list path by default")
	}
}

func TestRunGitHubRepoListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-repo-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubRepoListCommand([]string{"Aureuma", "--json"})
		if err != nil {
			t.Fatalf("runGitHubRepoListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github repo list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-repo-list" {
		t.Fatalf("expected delegated Rust github repo list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nrepo\nlist\nAureuma\n--json" {
		t.Fatalf("expected Rust CLI args to be github repo list + args, got %q", string(argsData))
	}
}

func TestRunGitHubRepoGetCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubRepoGetCommand([]string{"Aureuma/si", "--json"})
	if err != nil {
		t.Fatalf("runGitHubRepoGetCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github repo get path by default")
	}
}

func TestRunGitHubRepoGetCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-repo-get'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubRepoGetCommand([]string{"Aureuma/si", "--json"})
		if err != nil {
			t.Fatalf("runGitHubRepoGetCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github repo get to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-repo-get" {
		t.Fatalf("expected delegated Rust github repo get output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nrepo\nget\nAureuma/si\n--json" {
		t.Fatalf("expected Rust CLI args to be github repo get + args, got %q", string(argsData))
	}
}

func TestRunGitHubProjectCommandDefaultsToGoForMutationPath(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubProjectCommand([]string{"update", "PVT_123", "--json"})
	if err != nil {
		t.Fatalf("runGitHubProjectCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github project mutation path by default")
	}
}

func TestRunGitHubProjectCommandDelegatesToRustCLIForReadPath(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-project-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubProjectCommand([]string{"list", "Aureuma", "--json"})
		if err != nil {
			t.Fatalf("runGitHubProjectCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github project wrapper to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-project-wrapper" {
		t.Fatalf("expected delegated Rust github project output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nproject\nlist\nAureuma\n--json" {
		t.Fatalf("expected Rust CLI args to be github project + args, got %q", string(argsData))
	}
}

func TestRunGitHubProjectListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubProjectListCommand([]string{"Aureuma", "--json"})
	if err != nil {
		t.Fatalf("runGitHubProjectListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github project list path by default")
	}
}

func TestRunGitHubProjectListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-project-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubProjectListCommand([]string{"Aureuma", "--json"})
		if err != nil {
			t.Fatalf("runGitHubProjectListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github project list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-project-list" {
		t.Fatalf("expected delegated Rust github project list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nproject\nlist\nAureuma\n--json" {
		t.Fatalf("expected Rust CLI args to be github project list + args, got %q", string(argsData))
	}
}

func TestRunGitHubProjectGetCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubProjectGetCommand([]string{"PVT_123", "--json"})
	if err != nil {
		t.Fatalf("runGitHubProjectGetCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github project get path by default")
	}
}

func TestRunGitHubProjectGetCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-project-get'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubProjectGetCommand([]string{"PVT_123", "--json"})
		if err != nil {
			t.Fatalf("runGitHubProjectGetCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github project get to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-project-get" {
		t.Fatalf("expected delegated Rust github project get output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nproject\nget\nPVT_123\n--json" {
		t.Fatalf("expected Rust CLI args to be github project get + args, got %q", string(argsData))
	}
}

func TestRunGitHubProjectUpdateCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubProjectUpdateCommand([]string{"PVT_123", "--title", "Roadmap 2", "--json"})
	if err != nil {
		t.Fatalf("runGitHubProjectUpdateCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github project update path by default")
	}
}

func TestRunGitHubProjectUpdateCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-project-update'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubProjectUpdateCommand([]string{"PVT_123", "--title", "Roadmap 2", "--json"})
		if err != nil {
			t.Fatalf("runGitHubProjectUpdateCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github project update to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-project-update" {
		t.Fatalf("expected delegated Rust github project update output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nproject\nupdate\nPVT_123\n--title\nRoadmap 2\n--json" {
		t.Fatalf("expected Rust CLI args to be github project update + args, got %q", string(argsData))
	}
}

func TestRunGitHubProjectItemAddCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-project-item-add'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubProjectItemAddCommand([]string{"PVT_123", "--repo", "Aureuma/si", "--issue", "42", "--json"})
		if err != nil {
			t.Fatalf("runGitHubProjectItemAddCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github project item-add to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-project-item-add" {
		t.Fatalf("expected delegated Rust github project item-add output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nproject\nitem-add\nPVT_123\n--repo\nAureuma/si\n--issue\n42\n--json" {
		t.Fatalf("expected Rust CLI args to be github project item-add + args, got %q", string(argsData))
	}
}

func TestRunGitHubProjectItemSetCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-project-item-set'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubProjectItemSetCommand([]string{"PVT_123", "PVTI_1", "--field", "Status", "--text", "in progress", "--json"})
		if err != nil {
			t.Fatalf("runGitHubProjectItemSetCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github project item-set to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-project-item-set" {
		t.Fatalf("expected delegated Rust github project item-set output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nproject\nitem-set\nPVT_123\nPVTI_1\n--field\nStatus\n--text\nin progress\n--json" {
		t.Fatalf("expected Rust CLI args to be github project item-set + args, got %q", string(argsData))
	}
}

func TestRunGitHubProjectItemClearCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-project-item-clear'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubProjectItemClearCommand([]string{"PVT_123", "PVTI_1", "--field", "Status", "--json"})
		if err != nil {
			t.Fatalf("runGitHubProjectItemClearCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github project item-clear to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-project-item-clear" {
		t.Fatalf("expected delegated Rust github project item-clear output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nproject\nitem-clear\nPVT_123\nPVTI_1\n--field\nStatus\n--json" {
		t.Fatalf("expected Rust CLI args to be github project item-clear + args, got %q", string(argsData))
	}
}

func TestRunGitHubProjectItemArchiveCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-project-item-archive'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubProjectItemArchiveCommand([]string{"PVT_123", "PVTI_1", "--json"})
		if err != nil {
			t.Fatalf("runGitHubProjectItemArchiveCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github project item-archive to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-project-item-archive" {
		t.Fatalf("expected delegated Rust github project item-archive output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nproject\nitem-archive\nPVT_123\nPVTI_1\n--json" {
		t.Fatalf("expected Rust CLI args to be github project item-archive + args, got %q", string(argsData))
	}
}

func TestRunGitHubProjectItemUnarchiveCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-project-item-unarchive'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubProjectItemUnarchiveCommand([]string{"PVT_123", "PVTI_1", "--json"})
		if err != nil {
			t.Fatalf("runGitHubProjectItemUnarchiveCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github project item-unarchive to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-project-item-unarchive" {
		t.Fatalf("expected delegated Rust github project item-unarchive output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nproject\nitem-unarchive\nPVT_123\nPVTI_1\n--json" {
		t.Fatalf("expected Rust CLI args to be github project item-unarchive + args, got %q", string(argsData))
	}
}

func TestRunGitHubProjectItemDeleteCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-project-item-delete'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubProjectItemDeleteCommand([]string{"PVT_123", "PVTI_1", "--json"})
		if err != nil {
			t.Fatalf("runGitHubProjectItemDeleteCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github project item-delete to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-project-item-delete" {
		t.Fatalf("expected delegated Rust github project item-delete output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nproject\nitem-delete\nPVT_123\nPVTI_1\n--json" {
		t.Fatalf("expected Rust CLI args to be github project item-delete + args, got %q", string(argsData))
	}
}

func TestRunGitHubProjectFieldsCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubProjectFieldsCommand([]string{"PVT_123", "--json"})
	if err != nil {
		t.Fatalf("runGitHubProjectFieldsCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github project fields path by default")
	}
}

func TestRunGitHubProjectFieldsCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-project-fields'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubProjectFieldsCommand([]string{"PVT_123", "--json"})
		if err != nil {
			t.Fatalf("runGitHubProjectFieldsCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github project fields to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-project-fields" {
		t.Fatalf("expected delegated Rust github project fields output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nproject\nfields\nPVT_123\n--json" {
		t.Fatalf("expected Rust CLI args to be github project fields + args, got %q", string(argsData))
	}
}

func TestRunGitHubProjectItemsCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubProjectItemsCommand([]string{"PVT_123", "--json"})
	if err != nil {
		t.Fatalf("runGitHubProjectItemsCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github project items path by default")
	}
}

func TestRunGitHubProjectItemsCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-project-items'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubProjectItemsCommand([]string{"PVT_123", "--json"})
		if err != nil {
			t.Fatalf("runGitHubProjectItemsCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github project items to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-project-items" {
		t.Fatalf("expected delegated Rust github project items output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nproject\nitems\nPVT_123\n--json" {
		t.Fatalf("expected Rust CLI args to be github project items + args, got %q", string(argsData))
	}
}

func TestRunGitHubWorkflowCommandDelegatesToRustCLIForReadPath(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-workflow-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubWorkflowCommand([]string{"list", "Aureuma/si", "--json"})
		if err != nil {
			t.Fatalf("runGitHubWorkflowCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github workflow wrapper to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-workflow-wrapper" {
		t.Fatalf("expected delegated Rust github workflow output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nworkflow\nlist\nAureuma/si\n--json" {
		t.Fatalf("expected Rust CLI args to be github workflow + args, got %q", string(argsData))
	}
}

func TestRunGitHubWorkflowListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubWorkflowListCommand([]string{"Aureuma/si", "--json"})
	if err != nil {
		t.Fatalf("runGitHubWorkflowListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github workflow list path by default")
	}
}

func TestRunGitHubWorkflowListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-workflow-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubWorkflowListCommand([]string{"Aureuma/si", "--json"})
		if err != nil {
			t.Fatalf("runGitHubWorkflowListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github workflow list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-workflow-list" {
		t.Fatalf("expected delegated Rust github workflow list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nworkflow\nlist\nAureuma/si\n--json" {
		t.Fatalf("expected Rust CLI args to be github workflow list + args, got %q", string(argsData))
	}
}

func TestRunGitHubWorkflowRunsCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubWorkflowRunsCommand([]string{"Aureuma/si", "--json"})
	if err != nil {
		t.Fatalf("runGitHubWorkflowRunsCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github workflow runs path by default")
	}
}

func TestRunGitHubWorkflowRunsCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-workflow-runs'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubWorkflowRunsCommand([]string{"Aureuma/si", "--json"})
		if err != nil {
			t.Fatalf("runGitHubWorkflowRunsCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github workflow runs to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-workflow-runs" {
		t.Fatalf("expected delegated Rust github workflow runs output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nworkflow\nruns\nAureuma/si\n--json" {
		t.Fatalf("expected Rust CLI args to be github workflow runs + args, got %q", string(argsData))
	}
}

func TestRunGitHubWorkflowRunGetCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubWorkflowRunGetCommand([]string{"Aureuma/si", "21", "--json"})
	if err != nil {
		t.Fatalf("runGitHubWorkflowRunGetCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github workflow run get path by default")
	}
}

func TestRunGitHubWorkflowRunGetCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-workflow-run-get'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubWorkflowRunGetCommand([]string{"Aureuma/si", "21", "--json"})
		if err != nil {
			t.Fatalf("runGitHubWorkflowRunGetCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github workflow run get to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-workflow-run-get" {
		t.Fatalf("expected delegated Rust github workflow run get output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nworkflow\nrun\nget\nAureuma/si\n21\n--json" {
		t.Fatalf("expected Rust CLI args to be github workflow run get + args, got %q", string(argsData))
	}
}

func TestRunGitHubWorkflowLogsCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubWorkflowLogsCommand([]string{"Aureuma/si", "21", "--raw"})
	if err != nil {
		t.Fatalf("runGitHubWorkflowLogsCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github workflow logs path by default")
	}
}

func TestRunGitHubWorkflowLogsCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-workflow-logs'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubWorkflowLogsCommand([]string{"Aureuma/si", "21", "--raw"})
		if err != nil {
			t.Fatalf("runGitHubWorkflowLogsCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github workflow logs to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-workflow-logs" {
		t.Fatalf("expected delegated Rust github workflow logs output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nworkflow\nlogs\nAureuma/si\n21\n--raw" {
		t.Fatalf("expected Rust CLI args to be github workflow logs + args, got %q", string(argsData))
	}
}

func TestRunGitHubWorkflowDispatchCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-workflow-dispatch'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubWorkflowDispatchCommand([]string{"Aureuma/si", "ci.yml", "--ref", "main", "--json"})
		if err != nil {
			t.Fatalf("runGitHubWorkflowDispatchCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github workflow dispatch to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-workflow-dispatch" {
		t.Fatalf("expected delegated Rust github workflow dispatch output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nworkflow\ndispatch\nAureuma/si\nci.yml\n--ref\nmain\n--json" {
		t.Fatalf("expected Rust CLI args to be github workflow dispatch + args, got %q", string(argsData))
	}
}

func TestRunGitHubWorkflowRunCancelCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-workflow-run-cancel'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubWorkflowRunCancelCommand([]string{"Aureuma/si", "21", "--json"})
		if err != nil {
			t.Fatalf("runGitHubWorkflowRunCancelCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github workflow run cancel to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-workflow-run-cancel" {
		t.Fatalf("expected delegated Rust github workflow run cancel output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nworkflow\nrun\ncancel\nAureuma/si\n21\n--json" {
		t.Fatalf("expected Rust CLI args to be github workflow run cancel + args, got %q", string(argsData))
	}
}

func TestRunGitHubWorkflowRunRerunCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-workflow-run-rerun'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubWorkflowRunRerunCommand([]string{"Aureuma/si", "21", "--json"})
		if err != nil {
			t.Fatalf("runGitHubWorkflowRunRerunCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github workflow run rerun to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-workflow-run-rerun" {
		t.Fatalf("expected delegated Rust github workflow run rerun output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nworkflow\nrun\nrerun\nAureuma/si\n21\n--json" {
		t.Fatalf("expected Rust CLI args to be github workflow run rerun + args, got %q", string(argsData))
	}
}

func TestRunGitHubWorkflowWatchCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-workflow-watch'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubWorkflowWatchCommand([]string{"Aureuma/si", "21", "--json"})
		if err != nil {
			t.Fatalf("runGitHubWorkflowWatchCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github workflow watch to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-workflow-watch" {
		t.Fatalf("expected delegated Rust github workflow watch output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nworkflow\nwatch\nAureuma/si\n21\n--json" {
		t.Fatalf("expected Rust CLI args to be github workflow watch + args, got %q", string(argsData))
	}
}

func TestRunGitHubIssueCommandDefaultsToGoForMutationPath(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubIssueCommand([]string{"create", "Aureuma/si", "--json"})
	if err != nil {
		t.Fatalf("runGitHubIssueCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github issue mutation path by default")
	}
}

func TestRunGitHubIssueCommandDelegatesToRustCLIForReadPath(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-issue-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubIssueCommand([]string{"list", "Aureuma/si", "--json"})
		if err != nil {
			t.Fatalf("runGitHubIssueCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github issue wrapper to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-issue-wrapper" {
		t.Fatalf("expected delegated Rust github issue output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nissue\nlist\nAureuma/si\n--json" {
		t.Fatalf("expected Rust CLI args to be github issue + args, got %q", string(argsData))
	}
}

func TestRunGitHubIssueCreateCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-issue-create'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubIssueCreateCommand([]string{"Aureuma/si", "--title", "Rust issue", "--json"})
		if err != nil {
			t.Fatalf("runGitHubIssueCreateCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github issue create to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-issue-create" {
		t.Fatalf("expected delegated Rust github issue create output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nissue\ncreate\nAureuma/si\n--title\nRust issue\n--json" {
		t.Fatalf("expected Rust CLI args to be github issue create + args, got %q", string(argsData))
	}
}

func TestRunGitHubIssueCommentCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-issue-comment'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubIssueCommentCommand([]string{"Aureuma/si", "77", "--body", "looks good", "--json"})
		if err != nil {
			t.Fatalf("runGitHubIssueCommentCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github issue comment to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-issue-comment" {
		t.Fatalf("expected delegated Rust github issue comment output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nissue\ncomment\nAureuma/si\n77\n--body\nlooks good\n--json" {
		t.Fatalf("expected Rust CLI args to be github issue comment + args, got %q", string(argsData))
	}
}

func TestRunGitHubIssueCloseCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-issue-close'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubIssueCloseCommand([]string{"Aureuma/si", "77", "--json"})
		if err != nil {
			t.Fatalf("runGitHubIssueCloseCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github issue close to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-issue-close" {
		t.Fatalf("expected delegated Rust github issue close output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nissue\nclose\nAureuma/si\n77\n--json" {
		t.Fatalf("expected Rust CLI args to be github issue close + args, got %q", string(argsData))
	}
}

func TestRunGitHubIssueReopenCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-issue-reopen'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubIssueReopenCommand([]string{"Aureuma/si", "77", "--json"})
		if err != nil {
			t.Fatalf("runGitHubIssueReopenCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github issue reopen to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-issue-reopen" {
		t.Fatalf("expected delegated Rust github issue reopen output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nissue\nreopen\nAureuma/si\n77\n--json" {
		t.Fatalf("expected Rust CLI args to be github issue reopen + args, got %q", string(argsData))
	}
}

func TestRunGitHubBranchCommandDelegatesToRustCLIForReadPath(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-branch-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubBranchCommand([]string{"list", "Aureuma/si", "--json"})
		if err != nil {
			t.Fatalf("runGitHubBranchCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github branch wrapper to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-branch-wrapper" {
		t.Fatalf("expected delegated Rust github branch output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nbranch\nlist\nAureuma/si\n--json" {
		t.Fatalf("expected Rust CLI args to be github branch + args, got %q", string(argsData))
	}
}

func TestRunGitHubBranchListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubBranchListCommand([]string{"Aureuma/si", "--json"})
	if err != nil {
		t.Fatalf("runGitHubBranchListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github branch list path by default")
	}
}

func TestRunGitHubBranchListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-branch-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubBranchListCommand([]string{"Aureuma/si", "--json"})
		if err != nil {
			t.Fatalf("runGitHubBranchListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github branch list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-branch-list" {
		t.Fatalf("expected delegated Rust github branch list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nbranch\nlist\nAureuma/si\n--json" {
		t.Fatalf("expected Rust CLI args to be github branch list + args, got %q", string(argsData))
	}
}

func TestRunGitHubBranchGetCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubBranchGetCommand([]string{"Aureuma/si", "main", "--json"})
	if err != nil {
		t.Fatalf("runGitHubBranchGetCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github branch get path by default")
	}
}

func TestRunGitHubBranchGetCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-branch-get'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubBranchGetCommand([]string{"Aureuma/si", "main", "--json"})
		if err != nil {
			t.Fatalf("runGitHubBranchGetCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github branch get to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-branch-get" {
		t.Fatalf("expected delegated Rust github branch get output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nbranch\nget\nAureuma/si\nmain\n--json" {
		t.Fatalf("expected Rust CLI args to be github branch get + args, got %q", string(argsData))
	}
}

func TestRunGitHubBranchCreateCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-branch-create'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubBranchCreateCommand([]string{"Aureuma/si", "feature/rust", "--from", "main", "--json"})
		if err != nil {
			t.Fatalf("runGitHubBranchCreateCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github branch create to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-branch-create" {
		t.Fatalf("expected delegated Rust github branch create output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nbranch\ncreate\nAureuma/si\nfeature/rust\n--from\nmain\n--json" {
		t.Fatalf("expected Rust CLI args to be github branch create + args, got %q", string(argsData))
	}
}

func TestRunGitHubBranchDeleteCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-branch-delete'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubBranchDeleteCommand([]string{"Aureuma/si", "feature/rust", "--force", "--json"})
		if err != nil {
			t.Fatalf("runGitHubBranchDeleteCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github branch delete to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-branch-delete" {
		t.Fatalf("expected delegated Rust github branch delete output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nbranch\ndelete\nAureuma/si\nfeature/rust\n--force\n--json" {
		t.Fatalf("expected Rust CLI args to be github branch delete + args, got %q", string(argsData))
	}
}

func TestRunGitHubBranchProtectCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-branch-protect'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubBranchProtectCommand([]string{"Aureuma/si", "main", "--required-check", "ci", "--required-approvals", "2", "--json"})
		if err != nil {
			t.Fatalf("runGitHubBranchProtectCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github branch protect to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-branch-protect" {
		t.Fatalf("expected delegated Rust github branch protect output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nbranch\nprotect\nAureuma/si\nmain\n--required-check\nci\n--required-approvals\n2\n--json" {
		t.Fatalf("expected Rust CLI args to be github branch protect + args, got %q", string(argsData))
	}
}

func TestRunGitHubBranchUnprotectCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-branch-unprotect'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubBranchUnprotectCommand([]string{"Aureuma/si", "main", "--force", "--json"})
		if err != nil {
			t.Fatalf("runGitHubBranchUnprotectCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github branch unprotect to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-branch-unprotect" {
		t.Fatalf("expected delegated Rust github branch unprotect output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nbranch\nunprotect\nAureuma/si\nmain\n--force\n--json" {
		t.Fatalf("expected Rust CLI args to be github branch unprotect + args, got %q", string(argsData))
	}
}

func TestRunGitHubGitCredentialCommandDefaultsToGoForNonGet(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubGitCredentialCommand([]string{"store"})
	if err != nil {
		t.Fatalf("runGitHubGitCredentialCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github git credential store path by default")
	}
}

func TestRunGitHubGitCredentialCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-git-credential'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubGitCredentialCommand([]string{"get", "--auth-mode", "oauth"})
		if err != nil {
			t.Fatalf("runGitHubGitCredentialCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github git credential get to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-git-credential" {
		t.Fatalf("expected delegated Rust github git credential output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\ngit\ncredential\nget\n--auth-mode\noauth" {
		t.Fatalf("expected Rust CLI args to be github git credential get + flags, got %q", string(argsData))
	}
}

func TestRunGitHubGitCommandDefaultsToGoForNonMigratedSubtree(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubGitCommand([]string{"setup"})
	if err != nil {
		t.Fatalf("runGitHubGitCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github git setup path by default")
	}
}

func TestRunGitHubGitCommandDelegatesToRustCLIForCredentialGet(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-git-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubGitCommand([]string{"credential", "get", "--auth-mode", "oauth"})
		if err != nil {
			t.Fatalf("runGitHubGitCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github git wrapper to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-git-wrapper" {
		t.Fatalf("expected delegated Rust github git output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\ngit\ncredential\nget\n--auth-mode\noauth" {
		t.Fatalf("expected Rust CLI args to be github git credential get + flags, got %q", string(argsData))
	}
}

func TestRunGitHubIssueListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubIssueListCommand([]string{"Aureuma/si", "--json"})
	if err != nil {
		t.Fatalf("runGitHubIssueListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github issue list path by default")
	}
}

func TestRunGitHubIssueListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-issue-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubIssueListCommand([]string{"Aureuma/si", "--json"})
		if err != nil {
			t.Fatalf("runGitHubIssueListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github issue list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-issue-list" {
		t.Fatalf("expected delegated Rust github issue list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nissue\nlist\nAureuma/si\n--json" {
		t.Fatalf("expected Rust CLI args to be github issue list + args, got %q", string(argsData))
	}
}

func TestRunGitHubIssueGetCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubIssueGetCommand([]string{"Aureuma/si", "12", "--json"})
	if err != nil {
		t.Fatalf("runGitHubIssueGetCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github issue get path by default")
	}
}

func TestRunGitHubIssueGetCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-issue-get'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubIssueGetCommand([]string{"Aureuma/si", "12", "--json"})
		if err != nil {
			t.Fatalf("runGitHubIssueGetCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github issue get to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-issue-get" {
		t.Fatalf("expected delegated Rust github issue get output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nissue\nget\nAureuma/si\n12\n--json" {
		t.Fatalf("expected Rust CLI args to be github issue get + args, got %q", string(argsData))
	}
}

func TestRunGitHubPRCommandDefaultsToGoForMutationPath(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubPRCommand([]string{"create", "Aureuma/si", "--json"})
	if err != nil {
		t.Fatalf("runGitHubPRCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github pr mutation path by default")
	}
}

func TestRunGitHubPRCommandDelegatesToRustCLIForReadPath(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-pr-wrapper'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubPRCommand([]string{"list", "Aureuma/si", "--json"})
		if err != nil {
			t.Fatalf("runGitHubPRCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github pr wrapper to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-pr-wrapper" {
		t.Fatalf("expected delegated Rust github pr output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\npr\nlist\nAureuma/si\n--json" {
		t.Fatalf("expected Rust CLI args to be github pr + args, got %q", string(argsData))
	}
}

func TestRunGitHubPRListCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubPRListCommand([]string{"Aureuma/si", "--json"})
	if err != nil {
		t.Fatalf("runGitHubPRListCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github pr list path by default")
	}
}

func TestRunGitHubPRListCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-pr-list'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubPRListCommand([]string{"Aureuma/si", "--json"})
		if err != nil {
			t.Fatalf("runGitHubPRListCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github pr list to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-pr-list" {
		t.Fatalf("expected delegated Rust github pr list output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\npr\nlist\nAureuma/si\n--json" {
		t.Fatalf("expected Rust CLI args to be github pr list + args, got %q", string(argsData))
	}
}

func TestRunGitHubPRGetCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubPRGetCommand([]string{"Aureuma/si", "34", "--json"})
	if err != nil {
		t.Fatalf("runGitHubPRGetCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github pr get path by default")
	}
}

func TestRunGitHubPRGetCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-pr-get'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubPRGetCommand([]string{"Aureuma/si", "34", "--json"})
		if err != nil {
			t.Fatalf("runGitHubPRGetCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github pr get to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-pr-get" {
		t.Fatalf("expected delegated Rust github pr get output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\npr\nget\nAureuma/si\n34\n--json" {
		t.Fatalf("expected Rust CLI args to be github pr get + args, got %q", string(argsData))
	}
}

func TestRunGitHubPRCreateCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-pr-create'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubPRCreateCommand([]string{"Aureuma/si", "--head", "feature/rust", "--base", "main", "--title", "Rust PR", "--json"})
		if err != nil {
			t.Fatalf("runGitHubPRCreateCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github pr create to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-pr-create" {
		t.Fatalf("expected delegated Rust github pr create output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\npr\ncreate\nAureuma/si\n--head\nfeature/rust\n--base\nmain\n--title\nRust PR\n--json" {
		t.Fatalf("expected Rust CLI args to be github pr create + args, got %q", string(argsData))
	}
}

func TestRunGitHubPRCommentCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-pr-comment'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubPRCommentCommand([]string{"Aureuma/si", "35", "--body", "ship it", "--json"})
		if err != nil {
			t.Fatalf("runGitHubPRCommentCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github pr comment to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-pr-comment" {
		t.Fatalf("expected delegated Rust github pr comment output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\npr\ncomment\nAureuma/si\n35\n--body\nship it\n--json" {
		t.Fatalf("expected Rust CLI args to be github pr comment + args, got %q", string(argsData))
	}
}

func TestRunGitHubPRMergeCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-pr-merge'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubPRMergeCommand([]string{"Aureuma/si", "35", "--method", "squash", "--json"})
		if err != nil {
			t.Fatalf("runGitHubPRMergeCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github pr merge to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-pr-merge" {
		t.Fatalf("expected delegated Rust github pr merge output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\npr\nmerge\nAureuma/si\n35\n--method\nsquash\n--json" {
		t.Fatalf("expected Rust CLI args to be github pr merge + args, got %q", string(argsData))
	}
}

func TestRunGitHubRawCommandDefaultsToGoForNonGetMethod(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubRawCommand([]string{"--method", "POST", "--path", "/graphql", "--json"})
	if err != nil {
		t.Fatalf("runGitHubRawCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github raw path for non-GET method")
	}
}

func TestRunGitHubRawCommandDelegatesToRustCLIForGet(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-raw'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubRawCommand([]string{"--path", "/rate_limit", "--param", "scope=core", "--json"})
		if err != nil {
			t.Fatalf("runGitHubRawCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github raw command to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-raw" {
		t.Fatalf("expected delegated Rust github raw output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nraw\n--path\n/rate_limit\n--param\nscope=core\n--json" {
		t.Fatalf("expected Rust CLI args to be github raw + args, got %q", string(argsData))
	}
}

func TestRunGitHubGraphQLCommandDefaultsToGoForMutation(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubGraphQLCommand([]string{"--query", "mutation { viewer { login } }", "--json"})
	if err != nil {
		t.Fatalf("runGitHubGraphQLCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github graphql path for mutation query")
	}
}

func TestRunGitHubGraphQLCommandDelegatesToRustCLIForQuery(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-graphql'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubGraphQLCommand([]string{"--query", "query { viewer { login } }", "--json"})
		if err != nil {
			t.Fatalf("runGitHubGraphQLCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github graphql command to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-graphql" {
		t.Fatalf("expected delegated Rust github graphql output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\ngraphql\n--query\nquery { viewer { login } }\n--json" {
		t.Fatalf("expected Rust CLI args to be github graphql + args, got %q", string(argsData))
	}
}

func TestRunGitHubCommandDefaultsToGoForNonMigratedSubtree(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runGitHubCommand([]string{"repo", "create", "--json"})
	if err != nil {
		t.Fatalf("runGitHubCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go github root path for non-migrated subtree")
	}
}

func TestRunGitHubCommandDelegatesToRustCLIForMigratedReadPath(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-github-root'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runGitHubCommand([]string{"repo", "list", "Aureuma", "--json"})
		if err != nil {
			t.Fatalf("runGitHubCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected github root to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-github-root" {
		t.Fatalf("expected delegated Rust github root output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nrepo\nlist\nAureuma\n--json" {
		t.Fatalf("expected Rust CLI args to be github root + args, got %q", string(argsData))
	}
}
func TestMaybeRunRustVaultTrustLookupDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"found\":true,\"matches\":false,\"repo_root\":\"/repo\",\"file\":\"/repo/.env\",\"expected_fingerprint\":\"expected\",\"stored_fingerprint\":\"stored\",\"trusted_at\":\"2030-01-01T00:00:00Z\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	lookup, delegated, err := maybeRunRustVaultTrustLookup("/tmp/trust.json", "/repo", "/repo/.env", "expected")
	if err != nil {
		t.Fatalf("maybeRunRustVaultTrustLookup: %v", err)
	}
	if !delegated {
		t.Fatalf("expected vault trust lookup to delegate to Rust")
	}
	if !lookup.Found || lookup.Matches {
		t.Fatalf("unexpected lookup result: %+v", lookup)
	}
	if lookup.StoredFingerprint != "stored" {
		t.Fatalf("unexpected stored fingerprint: %+v", lookup)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "vault\ntrust\nlookup\n--path\n/tmp/trust.json\n--repo-root\n/repo\n--file\n/repo/.env\n--fingerprint\nexpected\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeLoadRustWarmupStateDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"version\":3,\"updated_at\":\"2030-01-01T00:00:00Z\",\"profiles\":{\"ferma\":{\"profile_id\":\"ferma\",\"last_result\":\"ready\",\"next_due\":\"2030-01-02T00:00:00Z\"}}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	state, delegated, err := maybeLoadRustWarmupState("/tmp/warmup-state.json")
	if err != nil {
		t.Fatalf("maybeLoadRustWarmupState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected warmup state to delegate to Rust")
	}
	if state.Version != 3 || state.Profiles["ferma"].LastResult != "ready" {
		t.Fatalf("unexpected state: %+v", state)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "warmup\nstatus\n--path\n/tmp/warmup-state.json\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeSaveRustWarmupStateDelegatesAndWritesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	delegated, err := maybeSaveRustWarmupState("/tmp/warmup-state.json", warmWeeklyState{
		Version: 3,
		Profiles: map[string]*warmWeeklyProfileState{
			"ferma": {ProfileID: "ferma", LastResult: "ready"},
		},
	})
	if err != nil {
		t.Fatalf("maybeSaveRustWarmupState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected warmup state save to delegate to Rust")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "warmup\nstate\nwrite\n--path\n/tmp/warmup-state.json\n--state-json\n") {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeReadRustWarmupMarkerStateDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"disabled\":true,\"autostart_present\":false}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	state, delegated, err := maybeReadRustWarmupMarkerState("/tmp/autostart.v1", "/tmp/disabled.v1")
	if err != nil {
		t.Fatalf("maybeReadRustWarmupMarkerState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected warmup marker state to delegate to Rust")
	}
	if !state.Disabled || state.AutostartPresent {
		t.Fatalf("unexpected marker state: %+v", state)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "warmup\nmarker\nshow\n--autostart-path\n/tmp/autostart.v1\n--disabled-path\n/tmp/disabled.v1\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeWriteRustWarmupAutostartMarkerDelegates(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	delegated, err := maybeWriteRustWarmupAutostartMarker("/tmp/autostart.v1")
	if err != nil {
		t.Fatalf("maybeWriteRustWarmupAutostartMarker: %v", err)
	}
	if !delegated {
		t.Fatalf("expected warmup autostart marker to delegate to Rust")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "warmup\nmarker\nwrite-autostart\n--path\n/tmp/autostart.v1" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeSetRustWarmupDisabledDelegates(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	delegated, err := maybeSetRustWarmupDisabled("/tmp/disabled.v1", true)
	if err != nil {
		t.Fatalf("maybeSetRustWarmupDisabled: %v", err)
	}
	if !delegated {
		t.Fatalf("expected warmup disabled marker to delegate to Rust")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "warmup\nmarker\nset-disabled\n--path\n/tmp/disabled.v1\n--disabled=true" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeReadRustWarmupAutostartDecisionDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"requested\":true,\"reason\":\"legacy_state\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	decision, delegated, err := maybeReadRustWarmupAutostartDecision("/tmp/state.json", "/tmp/autostart.v1", "/tmp/disabled.v1")
	if err != nil {
		t.Fatalf("maybeReadRustWarmupAutostartDecision: %v", err)
	}
	if !delegated {
		t.Fatalf("expected warmup autostart decision to delegate to Rust")
	}
	if !decision.Requested || decision.Reason != "legacy_state" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "warmup\nautostart-decision\n--state-path\n/tmp/state.json\n--autostart-path\n/tmp/autostart.v1\n--disabled-path\n/tmp/disabled.v1\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeRunRustWarmupStatusDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'warmup-status'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustWarmupStatus(true)
	if err != nil {
		t.Fatalf("maybeRunRustWarmupStatus: %v", err)
	}
	if !delegated {
		t.Fatalf("expected warmup status to delegate to Rust")
	}
	if strings.TrimSpace(output) != "warmup-status" {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "warmup\nstatus\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeLoadRustFortSessionStateDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"profile_id\":\"ferma\",\"agent_id\":\"si-codex-ferma\",\"session_id\":\"session-123\",\"host\":\"https://fort.example.test\",\"container_host\":\"http://fort.internal:8088\",\"access_token_path\":\"/tmp/access.token\",\"refresh_token_path\":\"/tmp/refresh.token\",\"access_expires_at\":\"2030-01-01T00:00:00Z\",\"refresh_expires_at\":\"2030-02-01T00:00:00Z\",\"updated_at\":\"2030-01-01T00:00:00Z\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	state, delegated, err := maybeLoadRustFortSessionState("/tmp/session.json")
	if err != nil {
		t.Fatalf("maybeLoadRustFortSessionState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort session state to delegate to Rust")
	}
	if state.ProfileID != "ferma" || state.SessionID != "session-123" {
		t.Fatalf("unexpected state: %+v", state)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "fort\nsession-state\nshow\n--path\n/tmp/session.json\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeClassifyRustFortSessionStateDelegatesAndParsesVariant(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"Revoked\":{\"snapshot\":{\"profile_id\":\"ferma\"},\"reason\":\"RefreshUnauthorized\"}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	classification, delegated, err := maybeClassifyRustFortSessionState("/tmp/session.json", 123)
	if err != nil {
		t.Fatalf("maybeClassifyRustFortSessionState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort session classification to delegate to Rust")
	}
	if classification.State != "revoked" || classification.Reason != "RefreshUnauthorized" {
		t.Fatalf("unexpected classification: %+v", classification)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "fort\nsession-state\nclassify\n--path\n/tmp/session.json\n--now-unix\n123\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeLoadRustFortRuntimeAgentStateDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"profile_id\":\"ferma\",\"pid\":4242,\"command_path\":\"/tmp/si\",\"started_at\":\"2030-01-01T00:00:00Z\",\"updated_at\":\"2030-01-01T00:00:01Z\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	state, delegated, err := maybeLoadRustFortRuntimeAgentState("/tmp/runtime-agent.json")
	if err != nil {
		t.Fatalf("maybeLoadRustFortRuntimeAgentState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort runtime agent state to delegate to Rust")
	}
	if state.ProfileID != "ferma" || state.PID != 4242 {
		t.Fatalf("unexpected state: %+v", state)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "fort\nruntime-agent-state\nshow\n--path\n/tmp/runtime-agent.json\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeSaveRustFortSessionStateDelegatesAndWritesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	delegated, err := maybeSaveRustFortSessionState("/tmp/session.json", fortProfileSessionState{
		ProfileID: "ferma",
		AgentID:   "si-codex-ferma",
		SessionID: "session-123",
	})
	if err != nil {
		t.Fatalf("maybeSaveRustFortSessionState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort session state write to delegate to Rust")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "fort\nsession-state\nwrite\n--path\n/tmp/session.json\n--state-json\n") {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
	if !strings.Contains(string(argsData), "\"profile_id\":\"ferma\"") {
		t.Fatalf("expected marshaled session state json in args, got %q", string(argsData))
	}
}

func TestMaybeSaveRustFortRuntimeAgentStateDelegatesAndWritesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	delegated, err := maybeSaveRustFortRuntimeAgentState("/tmp/runtime-agent.json", fortProfileRuntimeAgentState{
		ProfileID: "ferma",
		PID:       4242,
	})
	if err != nil {
		t.Fatalf("maybeSaveRustFortRuntimeAgentState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort runtime agent state write to delegate to Rust")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "fort\nruntime-agent-state\nwrite\n--path\n/tmp/runtime-agent.json\n--state-json\n") {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
	if !strings.Contains(string(argsData), "\"profile_id\":\"ferma\"") {
		t.Fatalf("expected marshaled runtime state json in args, got %q", string(argsData))
	}
}

func TestMaybeApplyRustFortSessionRefreshOutcomeDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"state\":{\"profile_id\":\"alpha\",\"agent_id\":\"si-codex-alpha\",\"session_id\":\"rfs_existing\",\"access_expires_at\":\"2030-01-01T00:00:00Z\",\"refresh_expires_at\":\"2030-02-01T00:00:00Z\"},\"classification\":{\"state\":\"resumable\"}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	transition, delegated, err := maybeApplyRustFortSessionRefreshOutcome("/tmp/session.json", fortSessionRefreshResult{
		AccessExpiresAt: "2030-01-01T00:00:00Z",
	}, time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatalf("maybeApplyRustFortSessionRefreshOutcome: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort refresh outcome to delegate to Rust")
	}
	if transition.State.AccessExpiresAt != "2030-01-01T00:00:00Z" {
		t.Fatalf("unexpected transitioned state: %+v", transition)
	}
	if transition.Classification.State != "resumable" {
		t.Fatalf("unexpected transitioned classification: %+v", transition)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "fort\nsession-state\nrefresh-outcome\n--path\n/tmp/session.json") {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
	if !strings.Contains(string(argsData), "--access-expires-at-unix\n1893456000") {
		t.Fatalf("expected access expiry unix arg, got %q", string(argsData))
	}
}

func TestMaybeRunRustFortSessionTeardownDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"state\":\"closed\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	classification, delegated, err := maybeRunRustFortSessionTeardown("/tmp/session.json", time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatalf("maybeRunRustFortSessionTeardown: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort teardown to delegate to Rust")
	}
	if classification.State != "closed" {
		t.Fatalf("unexpected teardown classification: %+v", classification)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "fort\nsession-state\nteardown\n--path\n/tmp/session.json\n--now-unix\n100\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeBuildRustDyadSpawnPlanDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"dyad\":\"alpha\",\"role\":\"ios\",\"network_name\":\"si\",\"workspace_host\":\"/workspace\",\"configs_host\":\"/configs-src\",\"codex_volume\":\"si-codex-alpha\",\"skills_volume\":\"si-codex-skills\",\"forward_ports\":\"1455-1465\",\"docker_socket\":true,\"actor\":{\"member\":\"actor\",\"container_name\":\"si-actor-alpha\",\"image\":\"actor:latest\",\"env\":[],\"bind_mounts\":[],\"command\":[]},\"critic\":{\"member\":\"critic\",\"container_name\":\"si-critic-alpha\",\"image\":\"critic:latest\",\"env\":[],\"bind_mounts\":[],\"command\":[]}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	plan, delegated, err := maybeBuildRustDyadSpawnPlan(rustDyadSpawnPlanRequest{
		Name:         "alpha",
		Role:         "ios",
		ActorImage:   "actor:latest",
		CriticImage:  "critic:latest",
		Workspace:    "/workspace",
		Configs:      "/configs-src",
		ForwardPorts: "1455-1465",
		DockerSocket: true,
	})
	if err != nil {
		t.Fatalf("maybeBuildRustDyadSpawnPlan: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad spawn plan to delegate to Rust")
	}
	if plan.Actor.ContainerName != "si-actor-alpha" || plan.Critic.ContainerName != "si-critic-alpha" {
		t.Fatalf("unexpected dyad plan: %+v", plan)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "dyad\nspawn-plan\n--name\nalpha") {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeStartRustDyadSpawnDelegatesToRustCLI(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'started'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	delegated, err := maybeStartRustDyadSpawn(rustDyadSpawnPlanRequest{
		Name:         "alpha",
		Role:         "ios",
		ActorImage:   "actor:latest",
		CriticImage:  "critic:latest",
		Workspace:    "/workspace",
		Configs:      "/configs-src",
		ForwardPorts: "1455-1465",
		DockerSocket: true,
	})
	if err != nil {
		t.Fatalf("maybeStartRustDyadSpawn: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad spawn start to delegate to Rust")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "dyad\nspawn-start\n--name\nalpha") {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
	if strings.Contains(string(argsData), "--format\njson") {
		t.Fatalf("did not expect spawn-start to pass --format json: %q", string(argsData))
	}
}

func TestMaybeRunRustDyadContainerActionDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'dyad-started'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustDyadContainerAction("start", "alpha")
	if err != nil {
		t.Fatalf("maybeRunRustDyadContainerAction: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad start to delegate to Rust")
	}
	if strings.TrimSpace(output) != "dyad-started" {
		t.Fatalf("unexpected output %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nstart\nalpha" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeRunRustDyadRemoveDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'dyad-removed'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustDyadRemove("alpha")
	if err != nil {
		t.Fatalf("maybeRunRustDyadRemove: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad remove to delegate to Rust")
	}
	if strings.TrimSpace(output) != "dyad-removed" {
		t.Fatalf("unexpected output %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nremove\nalpha" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeRunRustDyadExecDelegatesToRustCLI(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	delegated, err := maybeRunRustDyadExec("alpha", "critic", true, []string{"bash", "-lc", "echo hi"})
	if err != nil {
		t.Fatalf("maybeRunRustDyadExec: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad exec to delegate to Rust")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nexec\nalpha\n--member\ncritic\n--tty=true\n--\nbash\n-lc\necho hi" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeRunRustDyadCleanupDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'removed=2'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustDyadCleanup()
	if err != nil {
		t.Fatalf("maybeRunRustDyadCleanup: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad cleanup to delegate to Rust")
	}
	if strings.TrimSpace(output) != "removed=2" {
		t.Fatalf("unexpected output %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\ncleanup" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeRunRustDyadLogsDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'critic logs'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustDyadLogs("alpha", "critic", 42, true)
	if err != nil {
		t.Fatalf("maybeRunRustDyadLogs: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad logs to delegate to Rust")
	}
	if strings.TrimSpace(output) != "critic logs" {
		t.Fatalf("unexpected output %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nlogs\nalpha\n--member\ncritic\n--tail\n42\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeRunRustDyadListDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '[{\"dyad\":\"alpha\",\"role\":\"ios\",\"actor\":\"running\",\"critic\":\"exited\"}]'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustDyadList(true)
	if err != nil {
		t.Fatalf("maybeRunRustDyadList: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad list to delegate to Rust")
	}
	if !strings.Contains(output, "\"dyad\":\"alpha\"") {
		t.Fatalf("unexpected output %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nlist\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeRunRustDyadStatusDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'dyad-status'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustDyadStatus("alpha", true)
	if err != nil {
		t.Fatalf("maybeRunRustDyadStatus: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad status to delegate to Rust")
	}
	if strings.TrimSpace(output) != "dyad-status" {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nstatus\nalpha\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeReadRustDyadStatusDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"dyad\":\"alpha\",\"found\":true,\"actor\":{\"name\":\"si-actor-alpha\",\"id\":\"actor-id\",\"status\":\"running\"},\"critic\":{\"name\":\"si-critic-alpha\",\"id\":\"critic-id\",\"status\":\"exited\"}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	status, delegated, err := maybeReadRustDyadStatus("alpha")
	if err != nil {
		t.Fatalf("maybeReadRustDyadStatus: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad status to delegate to Rust")
	}
	if status == nil || status.Actor == nil || status.Actor.Status != "running" {
		t.Fatalf("unexpected status %+v", status)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nstatus\nalpha\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeReadRustDyadPeekPlanDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"dyad\":\"alpha\",\"member\":\"critic\",\"actor_container_name\":\"si-actor-alpha\",\"critic_container_name\":\"si-critic-alpha\",\"actor_session_name\":\"si-dyad-alpha-actor\",\"critic_session_name\":\"si-dyad-alpha-critic\",\"peek_session_name\":\"peek-main\",\"actor_attach_command\":\"actor-cmd\",\"critic_attach_command\":\"critic-cmd\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	plan, delegated, err := maybeReadRustDyadPeekPlan("alpha", "critic", "peek-main")
	if err != nil {
		t.Fatalf("maybeReadRustDyadPeekPlan: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad peek plan to delegate to Rust")
	}
	if plan == nil || plan.PeekSessionName != "peek-main" || plan.CriticAttachCommand != "critic-cmd" {
		t.Fatalf("unexpected plan %+v", plan)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\npeek-plan\nalpha\n--member\ncritic\n--format\njson\n--session\npeek-main" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeApplyRustFortUnauthorizedRefreshOutcomeDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"state\":{\"profile_id\":\"alpha\",\"agent_id\":\"si-codex-alpha\",\"session_id\":\"\"},\"classification\":{\"state\":\"revoked\",\"reason\":\"RefreshUnauthorized\"}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	transition, delegated, err := maybeApplyRustFortUnauthorizedRefreshOutcome("/tmp/session.json", time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatalf("maybeApplyRustFortUnauthorizedRefreshOutcome: %v", err)
	}
	if !delegated {
		t.Fatalf("expected unauthorized fort refresh outcome to delegate to Rust")
	}
	if transition.State.SessionID != "" || transition.Classification.State != "revoked" {
		t.Fatalf("unexpected unauthorized transition: %+v", transition)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "fort\nsession-state\nrefresh-outcome\n--path\n/tmp/session.json\n--outcome\nunauthorized\n--now-unix\n100\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeRunRustCodexLogsDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'log line'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustCodexLogs("ferma", "25", true)
	if err != nil {
		t.Fatalf("maybeRunRustCodexLogs: %v", err)
	}
	if !delegated {
		t.Fatalf("expected logs action to delegate to Rust")
	}
	if strings.TrimSpace(output) != "log line" {
		t.Fatalf("expected Rust logs output, got %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\ntail\nferma\n--tail\n25" {
		t.Fatalf("expected Rust CLI args to be codex tail ferma --tail 25, got %q", string(argsData))
	}
}

func TestMaybeRunRustCodexCloneResultDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"name\":\"ferma\",\"repo\":\"acme/repo\",\"container_name\":\"si-codex-ferma\",\"output\":\"cloned\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	result, delegated, err := maybeRunRustCodexCloneResult("ferma", "acme/repo", "token-123")
	if err != nil {
		t.Fatalf("maybeRunRustCodexCloneResult: %v", err)
	}
	if !delegated {
		t.Fatalf("expected clone result to delegate to Rust")
	}
	if result == nil || result.ContainerName != "si-codex-ferma" || result.Repo != "acme/repo" {
		t.Fatalf("unexpected result %#v", result)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nclone\nferma\nacme/repo\n--format\njson\n--gh-pat\ntoken-123" {
		t.Fatalf("unexpected Rust CLI args %q", string(argsData))
	}
}

func TestMaybeRunRustCodexExecDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'exec-output'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustCodexExec("ferma", "/workspace/project", true, false, []string{"A=1", "B=2"}, []string{"git", "status"})
	if err != nil {
		t.Fatalf("maybeRunRustCodexExec: %v", err)
	}
	if !delegated {
		t.Fatalf("expected exec action to delegate to Rust")
	}
	if strings.TrimSpace(output) != "exec-output" {
		t.Fatalf("expected Rust exec output, got %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nexec\nferma\n--interactive=true\n--tty=false\n--workdir\n/workspace/project\n--env\nA=1\n--env\nB=2\n--\ngit\nstatus" {
		t.Fatalf("expected Rust CLI args to be codex exec payload, got %q", string(argsData))
	}
}

func TestMaybeRunRustCodexListDelegatesForTextOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'si-codex-ferma\trunning\taureuma/si:local'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustCodexList(false)
	if err != nil {
		t.Fatalf("maybeRunRustCodexList: %v", err)
	}
	if !delegated {
		t.Fatalf("expected list action to delegate to Rust")
	}
	if strings.TrimSpace(output) != "si-codex-ferma\trunning\taureuma/si:local" {
		t.Fatalf("unexpected Rust list output %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nlist\n--format\ntext" {
		t.Fatalf("expected Rust CLI args to be codex list --format text, got %q", string(argsData))
	}
}

func TestMaybeRunRustCodexListDelegatesForJSONOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '[]'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustCodexList(true)
	if err != nil {
		t.Fatalf("maybeRunRustCodexList: %v", err)
	}
	if !delegated {
		t.Fatalf("expected json list action to delegate to Rust")
	}
	if strings.TrimSpace(output) != "[]" {
		t.Fatalf("unexpected Rust list output %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nlist\n--format\njson" {
		t.Fatalf("expected Rust CLI args to be codex list --format json, got %q", string(argsData))
	}
}

func TestMaybeReadRustCodexStatusDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"source\":\"app-server\",\"model\":\"gpt-5.2-codex\",\"five_hour_left_pct\":75,\"weekly_left_pct\":88}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	status, delegated, err := maybeReadRustCodexStatus("ferma", true)
	if err != nil {
		t.Fatalf("maybeReadRustCodexStatus: %v", err)
	}
	if !delegated {
		t.Fatalf("expected status action to delegate to Rust")
	}
	if status == nil || status.Model != "gpt-5.2-codex" || status.FiveHourLeftPct != 75 || status.WeeklyLeftPct != 88 {
		t.Fatalf("unexpected status payload: %#v", status)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nstatus-read\nferma\n--format\njson\n--raw" {
		t.Fatalf("expected Rust CLI args to be codex status-read ferma --format json --raw, got %q", string(argsData))
	}
}

func TestMaybeRunRustCodexStatusDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"model\":\"gpt-5.2-codex\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustCodexStatus("ferma", true, true)
	if err != nil {
		t.Fatalf("maybeRunRustCodexStatus: %v", err)
	}
	if !delegated {
		t.Fatalf("expected status command to delegate to Rust")
	}
	if !strings.Contains(output, "\"model\":\"gpt-5.2-codex\"") {
		t.Fatalf("unexpected output %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nstatus-read\nferma\n--format\njson\n--raw" {
		t.Fatalf("expected Rust CLI args to be codex status-read ferma --format json --raw, got %q", string(argsData))
	}
}

func TestMaybeBuildRustCodexRespawnPlanDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"effective_name\":\"ferma\",\"profile_id\":\"ferma\",\"remove_targets\":[\"alpha\",\"ferma\"]}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	plan, delegated, err := maybeBuildRustCodexRespawnPlan("ferma", "ferma", []string{"si-codex-alpha", "ferma"})
	if err != nil {
		t.Fatalf("maybeBuildRustCodexRespawnPlan: %v", err)
	}
	if !delegated {
		t.Fatalf("expected respawn plan to delegate to Rust")
	}
	if plan == nil || plan.EffectiveName != "ferma" || len(plan.RemoveTargets) != 2 || plan.RemoveTargets[0] != "alpha" {
		t.Fatalf("unexpected respawn plan payload: %#v", plan)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nrespawn-plan\nferma\n--format\njson\n--profile-id\nferma\n--profile-container\nsi-codex-alpha\n--profile-container\nferma" {
		t.Fatalf("unexpected respawn plan args %q", string(argsData))
	}
}

func TestMaybeReadRustCodexTmuxPlanDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"session_name\":\"si-codex-pane-profile-beta\",\"target\":\"si-codex-pane-profile-beta:0.0\",\"launch_command\":\"launch-cmd\",\"resume_command\":\"resume-cmd\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	plan, delegated, err := maybeReadRustCodexTmuxPlan("profile-beta", "/workspace/app", "sess-123", "profile-beta")
	if err != nil {
		t.Fatalf("maybeReadRustCodexTmuxPlan: %v", err)
	}
	if !delegated {
		t.Fatalf("expected codex tmux plan to delegate to Rust")
	}
	if plan == nil || plan.SessionName != "si-codex-pane-profile-beta" || plan.ResumeCommand != "resume-cmd" {
		t.Fatalf("unexpected tmux plan %#v", plan)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\ntmux-plan\nprofile-beta\n--format\njson\n--start-dir\n/workspace/app\n--resume-session-id\nsess-123\n--resume-profile\nprofile-beta" {
		t.Fatalf("unexpected tmux plan args %q", string(argsData))
	}
}

func TestMaybeReadRustCodexTmuxCommandDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"container\":\"abc123\",\"launch_command\":\"launch-cmd\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	command, delegated, err := maybeReadRustCodexTmuxCommand("abc123")
	if err != nil {
		t.Fatalf("maybeReadRustCodexTmuxCommand: %v", err)
	}
	if !delegated {
		t.Fatalf("expected codex tmux command to delegate to Rust")
	}
	if command == nil || command.Container != "abc123" || command.LaunchCommand != "launch-cmd" {
		t.Fatalf("unexpected tmux command %#v", command)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\ntmux-command\n--container\nabc123\n--format\njson" {
		t.Fatalf("unexpected tmux command args %q", string(argsData))
	}
}

func TestMaybeParseRustCodexReportCaptureDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	stdinPath := filepath.Join(dir, "stdin.json")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\ncat >" + shellSingleQuote(stdinPath) + "\nprintf '%s\\n' '{\"segments\":[{\"prompt\":\"first\",\"lines\":[\"line a\"],\"raw\":[\"line a\"]},{\"prompt\":\"second\",\"lines\":[\"• Done\",\"Worked for 5s\"],\"raw\":[\"• Done\",\"Worked for 5s\"]}],\"report\":\"• Done\\nWorked for 5s\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	parsed, delegated, err := maybeParseRustCodexReportCapture("› first\nline a\n› second\n• Done\nWorked for 5s", "› first\nline a\n› second\n• Done\nWorked for 5s", 1, false)
	if err != nil {
		t.Fatalf("maybeParseRustCodexReportCapture: %v", err)
	}
	if !delegated {
		t.Fatalf("expected codex report parse to delegate to Rust")
	}
	if parsed == nil || parsed.Report != "• Done\nWorked for 5s" || len(parsed.Segments) != 2 {
		t.Fatalf("unexpected report parse %#v", parsed)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nreport-parse\n--format\njson" {
		t.Fatalf("unexpected report parse args %q", string(argsData))
	}
	stdinData, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read stdin file: %v", err)
	}
	if !strings.Contains(string(stdinData), "\"prompt_index\":1") {
		t.Fatalf("expected stdin payload to contain prompt_index, got %q", string(stdinData))
	}
}

func TestBuildRustCodexSpawnPlanArgsIncludesPlannerFlags(t *testing.T) {
	args := buildRustCodexSpawnPlanArgs(rustCodexSpawnPlanRequest{
		Name:          "ferma",
		ProfileID:     "ferma",
		Workspace:     "/tmp/workspace",
		Workdir:       "/workspace/project",
		CodexVolume:   "si-codex-ferma",
		SkillsVolume:  "si-codex-skills",
		GHVolume:      "si-gh-ferma",
		Repo:          "acme/repo",
		GHPAT:         "token-123",
		DockerSocket:  true,
		Detach:        false,
		CleanSlate:    true,
		Image:         "aureuma/si:test",
		Network:       "si-test",
		VaultEnvFile:  "/tmp/workspace/.env",
		IncludeHostSI: true,
	})
	got := strings.Join(args, "\n")
	wantParts := []string{
		"codex",
		"spawn-plan",
		"--format",
		"json",
		"--workspace",
		"/tmp/workspace",
		"--name",
		"ferma",
		"--profile-id",
		"ferma",
		"--workdir",
		"/workspace/project",
		"--codex-volume",
		"si-codex-ferma",
		"--skills-volume",
		"si-codex-skills",
		"--gh-volume",
		"si-gh-ferma",
		"--repo",
		"acme/repo",
		"--gh-pat",
		"token-123",
		"--image",
		"aureuma/si:test",
		"--network",
		"si-test",
		"--vault-env-file",
		"/tmp/workspace/.env",
		"--docker-socket=true",
		"--detach=false",
		"--clean-slate=true",
		"--include-host-si=true",
	}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Fatalf("expected args to contain %q, got %q", part, got)
		}
	}
}

func TestMaybeBuildRustCodexSpawnPlanDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	plan := rustCodexSpawnPlan{
		Name:                   "ferma",
		ContainerName:          "si-codex-ferma",
		Image:                  "aureuma/si:test",
		NetworkName:            "si",
		WorkspaceHost:          "/tmp/workspace",
		WorkspacePrimaryTarget: "/workspace",
		WorkspaceMirrorTarget:  "/tmp/workspace",
		Workdir:                "/tmp/workspace",
		CodexVolume:            "si-codex-ferma",
		SkillsVolume:           "si-codex-skills",
		GHVolume:               "si-gh-ferma",
		DockerSocket:           true,
		Detach:                 true,
		Env:                    []string{"HOME=/home/si"},
		Mounts: []rustCodexSpawnPlanMount{
			{Source: "/tmp/workspace", Target: "/workspace"},
		},
	}
	payload, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s' " + shellSingleQuote(string(payload)) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	got, delegated, err := maybeBuildRustCodexSpawnPlan(rustCodexSpawnPlanRequest{
		Name:          "ferma",
		Workspace:     "/tmp/workspace",
		DockerSocket:  true,
		Detach:        true,
		IncludeHostSI: true,
	})
	if err != nil {
		t.Fatalf("maybeBuildRustCodexSpawnPlan: %v", err)
	}
	if !delegated {
		t.Fatalf("expected Rust spawn plan delegation")
	}
	if got == nil {
		t.Fatalf("expected spawn plan payload")
	}
	if got.ContainerName != "si-codex-ferma" {
		t.Fatalf("expected parsed container name, got %#v", got)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "codex\nspawn-plan") {
		t.Fatalf("expected codex spawn-plan invocation, got %q", string(argsData))
	}
}

func TestBuildRustCodexSpawnSpecArgsIncludesSpecFlags(t *testing.T) {
	args := buildRustCodexSpawnSpecArgs(rustCodexSpawnSpecRequest{
		rustCodexSpawnPlanRequest: rustCodexSpawnPlanRequest{
			Name:          "ferma",
			Workspace:     "/tmp/workspace",
			DockerSocket:  true,
			Detach:        true,
			CleanSlate:    false,
			IncludeHostSI: true,
		},
		Command: "echo hello",
		Env:     []string{"FORT_TOKEN=abc"},
		Labels:  []string{"si.codex.profile=ferma"},
		Ports:   []string{"3000:3000"},
	})
	got := strings.Join(args, "\n")
	if !strings.Contains(got, "codex\nspawn-spec") {
		t.Fatalf("expected spawn-spec subcommand, got %q", got)
	}
	if !strings.Contains(got, "--cmd\necho hello") {
		t.Fatalf("expected command flag, got %q", got)
	}
	if !strings.Contains(got, "--env\nFORT_TOKEN=abc") {
		t.Fatalf("expected env flag, got %q", got)
	}
	if !strings.Contains(got, "--label\nsi.codex.profile=ferma") {
		t.Fatalf("expected label flag, got %q", got)
	}
	if !strings.Contains(got, "--port\n3000:3000") {
		t.Fatalf("expected port flag, got %q", got)
	}
}

func TestMaybeBuildRustCodexSpawnSpecDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	spec := rustCodexSpawnSpec{
		Image:         "aureuma/si:test",
		Name:          "si-codex-ferma",
		Network:       "si",
		RestartPolicy: "unless-stopped",
		WorkingDir:    "/tmp/workspace",
		Command:       []string{"bash", "-lc", "echo hello"},
		Env:           []rustCodexSpawnSpecEnv{{Key: "HOME", Value: "/home/si"}},
		BindMounts:    []rustCodexSpawnPlanMount{{Source: "/tmp/workspace", Target: "/workspace"}},
		VolumeMounts:  []rustCodexSpawnSpecVolume{{Source: "si-codex-ferma", Target: "/home/si/.codex"}},
	}
	payload, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s' " + shellSingleQuote(string(payload)) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	got, delegated, err := maybeBuildRustCodexSpawnSpec(rustCodexSpawnSpecRequest{
		rustCodexSpawnPlanRequest: rustCodexSpawnPlanRequest{
			Name:          "ferma",
			Workspace:     "/tmp/workspace",
			DockerSocket:  true,
			Detach:        true,
			IncludeHostSI: true,
		},
		Command: "echo hello",
	})
	if err != nil {
		t.Fatalf("maybeBuildRustCodexSpawnSpec: %v", err)
	}
	if !delegated {
		t.Fatalf("expected Rust spawn spec delegation")
	}
	if got == nil || got.Name != "si-codex-ferma" {
		t.Fatalf("expected parsed spawn spec, got %#v", got)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "codex\nspawn-spec") {
		t.Fatalf("expected codex spawn-spec invocation, got %q", string(argsData))
	}
}

func TestBuildRustCodexSpawnStartArgsIncludesStartFlags(t *testing.T) {
	args := buildRustCodexSpawnStartArgs(rustCodexSpawnSpecRequest{
		rustCodexSpawnPlanRequest: rustCodexSpawnPlanRequest{
			Name:          "ferma",
			Workspace:     "/tmp/workspace",
			DockerSocket:  true,
			Detach:        true,
			IncludeHostSI: true,
		},
		Command: "echo hello",
		Env:     []string{"FORT_TOKEN=abc"},
		Labels:  []string{"si.codex.profile=ferma"},
		Ports:   []string{"3000:3000"},
	})
	got := strings.Join(args, "\n")
	if !strings.Contains(got, "codex\nspawn-start") {
		t.Fatalf("expected spawn-start subcommand, got %q", got)
	}
	if !strings.Contains(got, "--label\nsi.codex.profile=ferma") {
		t.Fatalf("expected label flag, got %q", got)
	}
}

func TestMaybeStartRustCodexSpawnDelegatesAndReturnsContainerID(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'container-id-123'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	got, delegated, err := maybeStartRustCodexSpawn(rustCodexSpawnSpecRequest{
		rustCodexSpawnPlanRequest: rustCodexSpawnPlanRequest{
			Name:          "ferma",
			Workspace:     "/tmp/workspace",
			DockerSocket:  true,
			Detach:        true,
			IncludeHostSI: true,
		},
		Command: "echo hello",
		Labels:  []string{"si.codex.profile=ferma"},
	})
	if err != nil {
		t.Fatalf("maybeStartRustCodexSpawn: %v", err)
	}
	if !delegated {
		t.Fatalf("expected Rust spawn-start delegation")
	}
	if got != "container-id-123" {
		t.Fatalf("expected container id, got %q", got)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "codex\nspawn-start") {
		t.Fatalf("expected codex spawn-start invocation, got %q", string(argsData))
	}
}

func TestMaybeBuildRustCodexRemoveArtifactsDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	payload := `{"name":"ferma","container_name":"si-codex-ferma","slug":"ferma","codex_volume":"si-codex-ferma","gh_volume":"si-gh-ferma"}`
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s' " + shellSingleQuote(payload) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	got, delegated, err := maybeBuildRustCodexRemoveArtifacts("ferma")
	if err != nil {
		t.Fatalf("maybeBuildRustCodexRemoveArtifacts: %v", err)
	}
	if !delegated {
		t.Fatalf("expected Rust remove-plan delegation")
	}
	if got == nil || got.ContainerName != "si-codex-ferma" || got.CodexVolume != "si-codex-ferma" {
		t.Fatalf("unexpected remove artifacts: %#v", got)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "codex\nremove-plan\nferma") {
		t.Fatalf("expected codex remove-plan invocation, got %q", string(argsData))
	}
}

func TestMaybeRunRustCodexRemoveDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'removed'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustCodexRemove("ferma", true)
	if err != nil {
		t.Fatalf("maybeRunRustCodexRemove: %v", err)
	}
	if !delegated {
		t.Fatalf("expected codex remove to delegate to Rust")
	}
	if strings.TrimSpace(output) != "removed" {
		t.Fatalf("unexpected output %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nremove\nferma\n--volumes" {
		t.Fatalf("unexpected Rust CLI args %q", string(argsData))
	}
}

func TestMaybeRunRustCodexRemoveResultDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"name\":\"ferma\",\"container_name\":\"si-codex-ferma\",\"profile_id\":\"ferma\",\"codex_volume\":\"si-codex-ferma\",\"gh_volume\":\"si-gh-ferma\",\"output\":\"removed\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	result, delegated, err := maybeRunRustCodexRemoveResult("ferma", true)
	if err != nil {
		t.Fatalf("maybeRunRustCodexRemoveResult: %v", err)
	}
	if !delegated {
		t.Fatalf("expected codex remove result to delegate to Rust")
	}
	if result == nil {
		t.Fatal("expected remove result")
	}
	if result.ContainerName != "si-codex-ferma" || result.ProfileID != "ferma" {
		t.Fatalf("unexpected result %#v", result)
	}
	if result.CodexVolume != "si-codex-ferma" || result.GHVolume != "si-gh-ferma" {
		t.Fatalf("unexpected result volumes %#v", result)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nremove\nferma\n--format\njson\n--volumes" {
		t.Fatalf("unexpected Rust CLI args %q", string(argsData))
	}
}

func TestMaybeRunRustCodexContainerActionResultDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"action\":\"stop\",\"name\":\"ferma\",\"container_name\":\"si-codex-ferma\",\"output\":\"stopped\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	result, delegated, err := maybeRunRustCodexContainerActionResult("stop", "ferma")
	if err != nil {
		t.Fatalf("maybeRunRustCodexContainerActionResult: %v", err)
	}
	if !delegated {
		t.Fatalf("expected codex action result to delegate to Rust")
	}
	if result == nil || result.ContainerName != "si-codex-ferma" || result.Action != "stop" {
		t.Fatalf("unexpected result %#v", result)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nstop\nferma\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args %q", string(argsData))
	}
}

func TestMaybeClearRustFortSessionStateDelegates(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	delegated, err := maybeClearRustFortSessionState("/tmp/session.json")
	if err != nil {
		t.Fatalf("maybeClearRustFortSessionState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort session clear to delegate to Rust")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "fort\nsession-state\nclear\n--path\n/tmp/session.json" {
		t.Fatalf("unexpected Rust CLI args %q", string(argsData))
	}
}

func TestMaybeClearRustFortRuntimeAgentStateDelegates(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	delegated, err := maybeClearRustFortRuntimeAgentState("/tmp/runtime-agent.json")
	if err != nil {
		t.Fatalf("maybeClearRustFortRuntimeAgentState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort runtime-agent clear to delegate to Rust")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "fort\nruntime-agent-state\nclear\n--path\n/tmp/runtime-agent.json" {
		t.Fatalf("unexpected Rust CLI args %q", string(argsData))
	}
}

func TestMaybeLoadRustCodexFortBootstrapDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	payload := `{"profile_id":"ferma","agent_id":"si-codex-ferma","session_id":"sess-1","host_url":"http://127.0.0.1:8088","container_host_url":"http://host.docker.internal:8088/","access_token_path":"/tmp/access.token","refresh_token_path":"/tmp/refresh.token","access_token_container_path":"/home/si/.si/access.token","refresh_token_container_path":"/home/si/.si/refresh.token"}`
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' " + shellSingleQuote(payload) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	boot, delegated, err := maybeLoadRustCodexFortBootstrap(
		"/tmp/session.json",
		"ferma",
		"/tmp/access.token",
		"/tmp/refresh.token",
		"/home/si/.si/access.token",
		"/home/si/.si/refresh.token",
	)
	if err != nil {
		t.Fatalf("maybeLoadRustCodexFortBootstrap: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort bootstrap view to delegate to Rust")
	}
	if boot == nil || boot.AgentID != "si-codex-ferma" || boot.ContainerHostURL != "http://host.docker.internal:8088/" {
		t.Fatalf("unexpected bootstrap view %#v", boot)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "fort\nsession-state\nbootstrap-view\n--path\n/tmp/session.json") {
		t.Fatalf("unexpected Rust CLI args %q", string(argsData))
	}
}
