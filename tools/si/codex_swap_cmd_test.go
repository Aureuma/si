package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexSwapWritesAuthAndPreservesConfig(t *testing.T) {
	home := t.TempDir()

	// Source auth cache for the selected profile.
	srcAuth := filepath.Join(home, ".si", "codex", "profiles", "cadma", "auth.json")
	if err := os.MkdirAll(filepath.Dir(srcAuth), 0o700); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	authBody, err := json.Marshal(profileAuthFile{Tokens: &profileAuthTokens{AccessToken: "access-token"}})
	if err != nil {
		t.Fatalf("marshal auth: %v", err)
	}
	if err := os.WriteFile(srcAuth, authBody, 0o600); err != nil {
		t.Fatalf("write auth: %v", err)
	}

	// Existing ~/.codex contents.
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatalf("mkdir .codex: %v", err)
	}
	cfgPath := filepath.Join(codexDir, "config.toml")
	cfgBody := []byte("model = \"gpt-5.2-codex\"\n")
	if err := os.WriteFile(cfgPath, cfgBody, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte("old-auth"), 0o600); err != nil {
		t.Fatalf("write old auth: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "junk.txt"), []byte("junk"), 0o600); err != nil {
		t.Fatalf("write junk: %v", err)
	}

	res, err := codexSwap(codexSwapOptions{
		Home:    home,
		Profile: codexProfile{ID: "cadma", Name: "Cadma", Email: "cadma@example.com"},
	})
	if err != nil {
		t.Fatalf("swap: %v", err)
	}
	if res.ProfileID != "cadma" {
		t.Fatalf("unexpected profile id %q", res.ProfileID)
	}

	// Old junk should be removed by the logout step.
	if _, err := os.Stat(filepath.Join(codexDir, "junk.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected junk.txt removed, stat err=%v", err)
	}

	gotCfg, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read preserved config: %v", err)
	}
	if string(gotCfg) != string(cfgBody) {
		t.Fatalf("unexpected preserved config content: %q", string(gotCfg))
	}

	gotAuth, err := os.ReadFile(filepath.Join(codexDir, "auth.json"))
	if err != nil {
		t.Fatalf("read swapped auth: %v", err)
	}
	if strings.TrimSpace(string(gotAuth)) != strings.TrimSpace(string(authBody)) {
		t.Fatalf("unexpected swapped auth content: %q", string(gotAuth))
	}
}

func TestCodexSwapCreatesDotCodexWhenMissing(t *testing.T) {
	home := t.TempDir()

	srcAuth := filepath.Join(home, ".si", "codex", "profiles", "america", "auth.json")
	if err := os.MkdirAll(filepath.Dir(srcAuth), 0o700); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	authBody, err := json.Marshal(profileAuthFile{Tokens: &profileAuthTokens{RefreshToken: "refresh-token"}})
	if err != nil {
		t.Fatalf("marshal auth: %v", err)
	}
	if err := os.WriteFile(srcAuth, authBody, 0o600); err != nil {
		t.Fatalf("write auth: %v", err)
	}

	if _, err := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("expected .codex missing before swap, stat err=%v", err)
	}

	if _, err := codexSwap(codexSwapOptions{
		Home:    home,
		Profile: codexProfile{ID: "america"},
	}); err != nil {
		t.Fatalf("swap: %v", err)
	}

	if _, err := os.Stat(filepath.Join(home, ".codex", "auth.json")); err != nil {
		t.Fatalf("expected auth.json to exist after swap: %v", err)
	}
}

func TestCodexSwapErrorsWhenAuthMissing(t *testing.T) {
	home := t.TempDir()

	_, err := codexSwap(codexSwapOptions{
		Home:    home,
		Profile: codexProfile{ID: "cadma"},
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "run `si login cadma`") {
		t.Fatalf("expected login hint, got: %v", err)
	}
}
