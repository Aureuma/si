package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexLogoutBlockedProfilesAddRemove(t *testing.T) {
	home := t.TempDir()
	if err := addCodexLogoutBlockedProfiles(home, []string{"Cadma", "cadma", "Einsteina"}); err != nil {
		t.Fatalf("add blocked profiles: %v", err)
	}

	blocked, err := loadCodexLogoutBlockedProfiles(home)
	if err != nil {
		t.Fatalf("load blocked profiles: %v", err)
	}
	if len(blocked) != 2 {
		t.Fatalf("expected deduped blocked profiles, got %#v", blocked)
	}
	if _, ok := blocked["cadma"]; !ok {
		t.Fatalf("expected cadma in blocked profiles, got %#v", blocked)
	}
	if _, ok := blocked["einsteina"]; !ok {
		t.Fatalf("expected einsteina in blocked profiles, got %#v", blocked)
	}

	if err := removeCodexLogoutBlockedProfiles(home, []string{"cadma", "einsteina"}); err != nil {
		t.Fatalf("remove blocked profiles: %v", err)
	}

	path, err := codexLogoutBlockedProfilesPath(home)
	if err != nil {
		t.Fatalf("blocked path: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected empty blocklist file removed, stat err=%v", err)
	}
}

func TestDiscoverCachedCodexProfileIDs(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".si", "codex", "profiles")
	if err := os.MkdirAll(filepath.Join(root, "cadma"), 0o700); err != nil {
		t.Fatalf("mkdir cadma: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "einsteina"), 0o700); err != nil {
		t.Fatalf("mkdir einsteina: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write helper file: %v", err)
	}

	ids := discoverCachedCodexProfileIDs(home)
	if len(ids) != 2 || ids[0] != "cadma" || ids[1] != "einsteina" {
		t.Fatalf("unexpected discovered ids: %#v", ids)
	}
}

func TestAddCodexLogoutBlockedProfilesMergesExistingEntries(t *testing.T) {
	home := t.TempDir()
	if err := addCodexLogoutBlockedProfiles(home, []string{"cadma"}); err != nil {
		t.Fatalf("seed blocked profiles: %v", err)
	}
	if err := addCodexLogoutBlockedProfiles(home, []string{"einsteina"}); err != nil {
		t.Fatalf("merge blocked profiles: %v", err)
	}

	blocked, err := loadCodexLogoutBlockedProfiles(home)
	if err != nil {
		t.Fatalf("load blocked profiles: %v", err)
	}
	if len(blocked) != 2 {
		t.Fatalf("expected merged blocked profiles, got %#v", blocked)
	}
	if _, ok := blocked["cadma"]; !ok {
		t.Fatalf("expected cadma in blocked profiles, got %#v", blocked)
	}
	if _, ok := blocked["einsteina"]; !ok {
		t.Fatalf("expected einsteina in blocked profiles, got %#v", blocked)
	}
}
