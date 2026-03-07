package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestVaultSetAcceptsTrailingFlagsAfterKeyValue(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess CLI test in short mode")
	}

	stateHome := t.TempDir()
	envFile := filepath.Join(t.TempDir(), ".env")
	env := map[string]string{
		"HOME":             stateHome,
		"SI_SETTINGS_HOME": stateHome,
	}

	scope := "trailing-set"
	_, stderr, err := runSICommand(
		t,
		env,
		"vault",
		"set",
		"TRAILING_SET_KEY",
		"set-value",
		"--env-file",
		envFile,
		"--scope",
		scope,
	)
	if err != nil {
		t.Fatalf("vault set failed: %v\nstderr=%s", err, stderr)
	}

	stdout, stderr, err := runSICommand(
		t,
		env,
		"vault",
		"get",
		"TRAILING_SET_KEY",
		"--env-file",
		envFile,
		"--scope",
		scope,
		"--reveal",
	)
	if err != nil {
		t.Fatalf("vault get failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "set-value" {
		t.Fatalf("unexpected vault get output: %q", strings.TrimSpace(stdout))
	}
}
