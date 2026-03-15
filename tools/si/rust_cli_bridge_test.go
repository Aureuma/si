package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
