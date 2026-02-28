package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyRemoteControlConfigSet(t *testing.T) {
	settings := Settings{}
	changed, err := applyRemoteControlConfigSet(&settings, remoteControlConfigSetInput{
		RepoProvided:          true,
		Repo:                  "/tmp/remote-control",
		BinProvided:           true,
		Bin:                   "/tmp/bin/remote-control",
		BuildProvided:         true,
		BuildRaw:              "true",
		SSHHostEnvKeyProvided: true,
		SSHHostEnvKey:         "SHAWN_MAC_HOST",
		SSHPortEnvKeyProvided: true,
		SSHPortEnvKey:         "SHAWN_MAC_PORT",
		SSHUserEnvKeyProvided: true,
		SSHUserEnvKey:         "SHAWN_MAC_USER",
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
	if settings.RemoteControl.Safari.SSH.HostEnvKey != "SHAWN_MAC_HOST" {
		t.Fatalf("unexpected host env key: %q", settings.RemoteControl.Safari.SSH.HostEnvKey)
	}
	if settings.RemoteControl.Safari.SSH.PortEnvKey != "SHAWN_MAC_PORT" {
		t.Fatalf("unexpected port env key: %q", settings.RemoteControl.Safari.SSH.PortEnvKey)
	}
	if settings.RemoteControl.Safari.SSH.UserEnvKey != "SHAWN_MAC_USER" {
		t.Fatalf("unexpected user env key: %q", settings.RemoteControl.Safari.SSH.UserEnvKey)
	}
}

func TestApplyRemoteControlConfigSetInvalidBuild(t *testing.T) {
	settings := Settings{}
	_, err := applyRemoteControlConfigSet(&settings, remoteControlConfigSetInput{
		BuildProvided: true,
		BuildRaw:      "not-bool",
	})
	if err == nil {
		t.Fatalf("expected invalid --build parse error")
	}
}

func TestApplyRemoteControlConfigSetClearsBuild(t *testing.T) {
	settings := Settings{}
	_, err := applyRemoteControlConfigSet(&settings, remoteControlConfigSetInput{
		BuildProvided: true,
		BuildRaw:      "true",
	})
	if err != nil {
		t.Fatalf("seed build=true: %v", err)
	}
	changed, err := applyRemoteControlConfigSet(&settings, remoteControlConfigSetInput{
		BuildProvided: true,
		BuildRaw:      "",
	})
	if err != nil {
		t.Fatalf("clear build: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if settings.RemoteControl.Build != nil {
		t.Fatalf("expected build pointer to be cleared")
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

func TestDefaultRemoteControlRepoPathFromWorkingDir(t *testing.T) {
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(origWD) }()

	base := t.TempDir()
	repo := filepath.Join(base, "remote-control")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	if got := defaultRemoteControlRepoPath(); got != repo {
		t.Fatalf("defaultRemoteControlRepoPath()=%q want %q", got, repo)
	}
}

func TestDefaultRemoteControlRepoPathFromHome(t *testing.T) {
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(origWD) }()

	home := t.TempDir()
	repo := filepath.Join(home, "Development", "remote-control")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Setenv("HOME", home)
	if got := defaultRemoteControlRepoPath(); got != repo {
		t.Fatalf("defaultRemoteControlRepoPath()=%q want %q", got, repo)
	}
}

func TestDetectRemoteControlBinaryFallback(t *testing.T) {
	t.Setenv("PATH", "")
	repo := filepath.Join(t.TempDir(), "repo")
	got := detectRemoteControlBinary(repo)
	want := filepath.Join(repo, "bin", "remote-control")
	if got != want {
		t.Fatalf("detectRemoteControlBinary()=%q want %q", got, want)
	}
}

func TestResolveRemoteControlConfigReference(t *testing.T) {
	settings := Settings{}
	orig := resolveRemoteControlVaultKeyValue
	resolveRemoteControlVaultKeyValue = func(_ Settings, key string) (string, bool) {
		if key == "KEY_FROM_VAULT" {
			return "vault-value", true
		}
		return "", false
	}
	defer func() { resolveRemoteControlVaultKeyValue = orig }()

	t.Setenv("KEY_FROM_ENV", "env-value")
	if got := resolveRemoteControlConfigReference(settings, "KEY_FROM_ENV", ""); got != "env-value" {
		t.Fatalf("env key resolve mismatch: %q", got)
	}
	if got := resolveRemoteControlConfigReference(settings, "", "env:KEY_FROM_ENV"); got != "env-value" {
		t.Fatalf("env: ref resolve mismatch: %q", got)
	}
	if got := resolveRemoteControlConfigReference(settings, "", "${KEY_FROM_ENV}"); got != "env-value" {
		t.Fatalf("${} ref resolve mismatch: %q", got)
	}
	if got := resolveRemoteControlConfigReference(settings, "KEY_FROM_VAULT", ""); got != "vault-value" {
		t.Fatalf("vault key resolve mismatch: %q", got)
	}
	if got := resolveRemoteControlConfigReference(settings, "", "literal-host"); got != "literal-host" {
		t.Fatalf("literal resolve mismatch: %q", got)
	}
}
