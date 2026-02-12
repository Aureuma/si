package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveHomeChildDirRefusesOutsideHome(t *testing.T) {
	home := t.TempDir()
	other := t.TempDir()
	target := filepath.Join(other, ".codex")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := removeHomeChildDir(home, target); err == nil {
		t.Fatalf("expected error removing outside-home path")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected target to still exist: %v", err)
	}
}

func TestRemoveHomeChildDirRemovesDirectory(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".codex")
	if err := os.MkdirAll(filepath.Join(target, "nested"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "nested", "a.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	removed, err := removeHomeChildDir(home, target)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !removed {
		t.Fatalf("expected removed=true")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected removed directory, got stat err=%v", err)
	}
}

func TestCodexLogoutRemovesDotCodex(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".codex")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	res, err := codexLogout(codexLogoutOptions{Home: home})
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	if len(res.Removed) != 1 || res.Removed[0] != target {
		t.Fatalf("unexpected removed: %#v", res.Removed)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected .codex removed, stat err=%v", err)
	}
}

func TestCodexLogoutPreservesConfigToml(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatalf("mkdir .codex: %v", err)
	}
	cfg := filepath.Join(codexDir, "config.toml")
	cfgBody := []byte("model = \"gpt-5.2-codex\"\n")
	if err := os.WriteFile(cfg, cfgBody, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte("token"), 0o600); err != nil {
		t.Fatalf("write auth: %v", err)
	}

	res, err := codexLogout(codexLogoutOptions{Home: home})
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	if len(res.Removed) != 1 || res.Removed[0] != codexDir {
		t.Fatalf("unexpected removed: %#v", res.Removed)
	}
	if len(res.Preserved) != 1 || res.Preserved[0] != cfg {
		t.Fatalf("unexpected preserved: %#v", res.Preserved)
	}
	if _, err := os.Stat(filepath.Join(codexDir, "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("expected auth file removed, stat err=%v", err)
	}
	gotCfg, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("read preserved config: %v", err)
	}
	if string(gotCfg) != string(cfgBody) {
		t.Fatalf("unexpected preserved config content: %q", string(gotCfg))
	}
}
