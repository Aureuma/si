package main

import (
	"os"
	"path/filepath"
	"strings"
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

func TestRunImageBuildPreflightFailureIncludesScriptLogs(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "tools", "si-image", "preflight-codex-upgrade.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\necho preflight failed details\nexit 3\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	err := runImageBuildPreflight(root)
	if err == nil {
		t.Fatalf("expected script failure error")
	}
	if !strings.Contains(err.Error(), "preflight failed details") {
		t.Fatalf("expected error to include script logs, got: %v", err)
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

func TestRunImageBuildPreflightSetsGoBinEnv(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "tools", "si-image", "preflight-codex-upgrade.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\nset -euo pipefail\n[[ -n \"${SI_GO_BIN:-}\" ]]\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if err := runImageBuildPreflight(root); err != nil {
		t.Fatalf("expected SI_GO_BIN env to be set, got error: %v", err)
	}
}
