package main

import (
	"strings"
	"testing"
)

func TestApplyRemoteControlConfigSet(t *testing.T) {
	settings := Settings{}
	changed, err := applyRemoteControlConfigSet(&settings, remoteControlConfigSetInput{
		RepoProvided:  true,
		Repo:          "/tmp/remote-control",
		BinProvided:   true,
		Bin:           "/tmp/bin/remote-control",
		BuildProvided: true,
		BuildRaw:      "true",
	})
	if err != nil {
		t.Fatalf("applyRemoteControlConfigSet err: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if settings.RemoteControl.Repo != "/tmp/remote-control" {
		t.Fatalf("unexpected repo: %q", settings.RemoteControl.Repo)
	}
	if settings.RemoteControl.Bin != "/tmp/bin/remote-control" {
		t.Fatalf("unexpected bin: %q", settings.RemoteControl.Bin)
	}
	if settings.RemoteControl.Build == nil || !*settings.RemoteControl.Build {
		t.Fatalf("expected build=true")
	}
}

func TestSettingsModulePathRemoteControl(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := settingsModulePath(settingsModuleRemoteControl)
	if err != nil {
		t.Fatalf("settingsModulePath(remote-control): %v", err)
	}
	if !strings.HasSuffix(path, "/remote-control/si.settings.toml") {
		t.Fatalf("expected remote-control settings suffix, got %q", path)
	}
}
