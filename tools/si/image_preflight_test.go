package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunImageBuildPreflightScriptMissing(t *testing.T) {
	root := t.TempDir()
	if err := runImageBuildPreflight(root); err == nil {
		t.Fatalf("expected missing script error")
	}
}

func TestRunImageBuildPreflightScriptFailure(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "tools", "si-image", "preflight-codex-upgrade.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\nexit 2\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if err := runImageBuildPreflight(root); err == nil {
		t.Fatalf("expected script failure error")
	}
}

func TestRunImageBuildPreflightScriptSuccess(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "tools", "si-image", "preflight-codex-upgrade.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\nset -e\necho ok >/dev/null\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if err := runImageBuildPreflight(root); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
}

