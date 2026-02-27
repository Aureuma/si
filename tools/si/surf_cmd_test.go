package main

import "testing"

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
