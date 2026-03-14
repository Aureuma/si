package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWorkspaceDirectoryFallsBackFromStaleSettingsToCWD(t *testing.T) {
	cwd := t.TempDir()
	settings := defaultSettings()
	settings.Codex.Workspace = filepath.Join(t.TempDir(), "missing")

	resolved, err := resolveWorkspaceDirectory(
		workspaceScopeCodex,
		false,
		"",
		"",
		&settings,
		cwd,
	)
	if err != nil {
		t.Fatalf("resolveWorkspaceDirectory returned error: %v", err)
	}
	if resolved.Path != cwd {
		t.Fatalf("expected cwd fallback %q, got %q", cwd, resolved.Path)
	}
	if !resolved.StaleSettings {
		t.Fatalf("expected stale settings flag to be set")
	}
	if resolved.Source != resolvedPathSourceCwd {
		t.Fatalf("expected cwd source, got %q", resolved.Source)
	}
}

func TestResolveDyadConfigsDirectoryFallsBackToBundledConfigs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		_ = os.Chdir(origWD)
	}()
	if err := os.Chdir(home); err != nil {
		t.Fatalf("chdir temp home: %v", err)
	}

	settings := defaultSettings()
	resolved, err := resolveDyadConfigsDirectory(false, "", "", &settings, home)
	if err != nil {
		t.Fatalf("resolveDyadConfigsDirectory returned error: %v", err)
	}
	if resolved.Source != resolvedPathSourceBundled {
		t.Fatalf("expected bundled source, got %q", resolved.Source)
	}
	if !strings.HasPrefix(resolved.Path, filepath.Join(home, ".si")) {
		t.Fatalf("expected bundled configs under ~/.si, got %q", resolved.Path)
	}
	templatePath := filepath.Join(resolved.Path, "codex-config.template.toml")
	if _, err := os.Stat(templatePath); err != nil {
		t.Fatalf("expected bundled template at %q: %v", templatePath, err)
	}
}

func TestResolveWorkspaceRootDirectoryFallsBackFromStaleSettingsToRepoParent(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "si")
	cwd := filepath.Join(repo, "tools", "si")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	settings := defaultSettings()
	settings.Paths.WorkspaceRoot = filepath.Join(t.TempDir(), "missing")

	resolved, err := resolveWorkspaceRootDirectory(false, "", "", &settings, cwd)
	if err != nil {
		t.Fatalf("resolveWorkspaceRootDirectory returned error: %v", err)
	}
	if resolved.Path != root {
		t.Fatalf("expected inferred root %q, got %q", root, resolved.Path)
	}
	if !resolved.StaleSettings {
		t.Fatalf("expected stale settings flag to be set")
	}
	if resolved.Source != resolvedPathSourceCwd {
		t.Fatalf("expected cwd source, got %q", resolved.Source)
	}
}
