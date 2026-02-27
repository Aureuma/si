package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplySurfConfigSet(t *testing.T) {
	settings := defaultSettings()
	changed, err := applySurfConfigSet(&settings, surfConfigSetInput{
		RepoProvided:           true,
		Repo:                   "/tmp/surf",
		BinProvided:            true,
		Bin:                    "/tmp/surf/bin/surf",
		BuildProvided:          true,
		BuildRaw:               "true",
		SettingsFileProvided:   true,
		SettingsFile:           "/tmp/surf/settings.toml",
		StateDirProvided:       true,
		StateDir:               "/tmp/surf/state",
		TunnelNameProvided:     true,
		TunnelName:             "surf-tunnel",
		TunnelModeProvided:     true,
		TunnelMode:             "token",
		TunnelVaultKeyProvided: true,
		TunnelVaultKey:         "SURF_TUNNEL_TOKEN",
	})
	if err != nil {
		t.Fatalf("applySurfConfigSet: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if settings.Surf.Repo != "/tmp/surf" || settings.Surf.Bin != "/tmp/surf/bin/surf" {
		t.Fatalf("unexpected repo/bin: %#v", settings.Surf)
	}
	if settings.Surf.Build == nil || !*settings.Surf.Build {
		t.Fatalf("expected build=true")
	}
	if settings.Surf.Tunnel.Mode != "token" || settings.Surf.Tunnel.Name != "surf-tunnel" {
		t.Fatalf("unexpected tunnel settings: %#v", settings.Surf.Tunnel)
	}
}

func TestApplySurfConfigSetClearsBuildOnEmpty(t *testing.T) {
	settings := defaultSettings()
	settings.Surf.Build = boolPtr(true)
	changed, err := applySurfConfigSet(&settings, surfConfigSetInput{BuildProvided: true, BuildRaw: ""})
	if err != nil {
		t.Fatalf("applySurfConfigSet: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if settings.Surf.Build != nil {
		t.Fatalf("expected build to be unset")
	}
}

func TestApplySurfConfigSetRejectsInvalidTunnelMode(t *testing.T) {
	settings := defaultSettings()
	_, err := applySurfConfigSet(&settings, surfConfigSetInput{TunnelModeProvided: true, TunnelMode: "invalid"})
	if err == nil {
		t.Fatalf("expected error for invalid tunnel mode")
	}
}

func TestApplySurfSettingsEnv(t *testing.T) {
	settings := defaultSettings()
	settings.Surf.SettingsFile = "/tmp/surf/settings.toml"
	settings.Surf.StateDir = "/tmp/surf/state"
	settings.Surf.Tunnel.Name = "surf-tunnel"
	settings.Surf.Tunnel.Mode = "token"
	settings.Surf.Tunnel.VaultKey = "SURF_TUNNEL_TOKEN"
	env := applySurfSettingsEnv(settings, []string{"PATH=/usr/bin", "SURF_STATE_DIR=/custom"})
	if !envHasValue(env, "SURF_SETTINGS_FILE") {
		t.Fatalf("expected SURF_SETTINGS_FILE to be added")
	}
	if !envHasValue(env, "SURF_TUNNEL_NAME") || !envHasValue(env, "SURF_TUNNEL_MODE") || !envHasValue(env, "SURF_TUNNEL_VAULT_KEY") {
		t.Fatalf("expected tunnel env values to be added")
	}
	count := 0
	for _, item := range env {
		if item == "SURF_STATE_DIR=/custom" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected existing SURF_STATE_DIR to be preserved once, got %d", count)
	}
}

func TestDefaultSurfRepoPathFindsSiblingRepo(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		_ = os.Chdir(orig)
	}()

	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "surf"), 0o755); err != nil {
		t.Fatalf("mkdir surf sibling: %v", err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatalf("chdir base: %v", err)
	}

	got := defaultSurfRepoPath()
	want := filepath.Join(base, "surf")
	if got != want {
		t.Fatalf("defaultSurfRepoPath()=%q want=%q", got, want)
	}
}

func TestDefaultSurfRepoPathEmptyWhenMissing(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		_ = os.Chdir(orig)
	}()

	base := t.TempDir()
	if err := os.Chdir(base); err != nil {
		t.Fatalf("chdir base: %v", err)
	}

	got := defaultSurfRepoPath()
	if got != "" {
		t.Fatalf("defaultSurfRepoPath()=%q want empty", got)
	}
}
