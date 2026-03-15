package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderVaultPreCommitHookUsesPortableLookupOrder(t *testing.T) {
	script := renderVaultPreCommitHook()

	if !strings.Contains(script, "# si-vault:hook pre-commit v2") {
		t.Fatalf("expected v2 managed marker, got: %s", script)
	}
	if strings.Contains(script, "SI_BIN_DEFAULT=") {
		t.Fatalf("hook should not hardcode installer executable path: %s", script)
	}
	if !strings.Contains(script, "git rev-parse --show-toplevel") {
		t.Fatalf("expected repo root lookup in hook: %s", script)
	}
	if !strings.Contains(script, "\"$repo_root/si\" vault check --staged --all") {
		t.Fatalf("expected repo-local si fallback in hook: %s", script)
	}
	if !strings.Contains(script, "exec si vault check --staged --all") {
		t.Fatalf("expected PATH fallback in hook: %s", script)
	}
}

func TestWriteHookFileRewritesExistingManagedHook(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pre-commit")
	legacy := "#!/bin/sh\nset -e\n# si-vault:hook pre-commit v1\nSI_BIN_DEFAULT='/tmp/old-si'\n"
	if err := writeHookFile(path, legacy, false); err != nil {
		t.Fatalf("write legacy hook: %v", err)
	}

	portablyRendered := renderVaultPreCommitHook()
	if err := writeHookFile(path, portablyRendered, false); err != nil {
		t.Fatalf("rewrite managed hook: %v", err)
	}

	data, err := readLocalFile(path)
	if err != nil {
		t.Fatalf("read rewritten hook: %v", err)
	}
	if strings.Contains(string(data), "SI_BIN_DEFAULT=") {
		t.Fatalf("expected hardcoded path to be removed, got: %s", string(data))
	}
	if !strings.Contains(string(data), "# si-vault:hook pre-commit v2") {
		t.Fatalf("expected rewritten hook marker to be updated, got: %s", string(data))
	}
}
