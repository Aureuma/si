package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFortArgsContainCredentialFlags(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{args: []string{"doctor"}, want: false},
		{args: []string{"--token", "abc", "doctor"}, want: true},
		{args: []string{"--token=abc", "doctor"}, want: true},
		{args: []string{"--token-file", "/tmp/tok", "doctor"}, want: true},
		{args: []string{"--token-file=/tmp/tok", "doctor"}, want: true},
	}
	for _, tc := range tests {
		if got := fortArgsContainCredentialFlags(tc.args); got != tc.want {
			t.Fatalf("fortArgsContainCredentialFlags(%v)=%v want=%v", tc.args, got, tc.want)
		}
	}
}

func TestFortRejectDeprecatedTokenValueEnv(t *testing.T) {
	t.Setenv("FORT_TOKEN", "legacy-token")
	if err := fortRejectDeprecatedTokenValueEnv(); err == nil {
		t.Fatalf("expected FORT_TOKEN to be rejected")
	}

	t.Setenv("FORT_TOKEN", "")
	t.Setenv("FORT_REFRESH_TOKEN", "legacy-refresh-token")
	if err := fortRejectDeprecatedTokenValueEnv(); err == nil {
		t.Fatalf("expected FORT_REFRESH_TOKEN to be rejected")
	}
}

func TestFortSanitizedEnvRemovesTokenEntries(t *testing.T) {
	out := fortSanitizedEnv([]string{
		"PATH=/usr/bin",
		"FORT_TOKEN=legacy",
		"FORT_REFRESH_TOKEN=legacy-refresh",
		"FORT_BOOTSTRAP_TOKEN_FILE=/tmp/bootstrap.token",
		"FORT_TOKEN_FILE=/tmp/legacy-admin.token",
		"FORT_TOKEN_PATH=/tmp/access.token",
	})
	joined := strings.Join(out, "\n")
	if strings.Contains(joined, "FORT_TOKEN=") || strings.Contains(joined, "FORT_REFRESH_TOKEN=") {
		t.Fatalf("token values leaked in sanitized env: %q", joined)
	}
	if strings.Contains(joined, "FORT_BOOTSTRAP_TOKEN_FILE=") || strings.Contains(joined, "FORT_TOKEN_FILE=") {
		t.Fatalf("token file vars leaked in sanitized env: %q", joined)
	}
	if !strings.Contains(joined, "PATH=/usr/bin") || !strings.Contains(joined, "FORT_TOKEN_PATH=/tmp/access.token") {
		t.Fatalf("unexpected sanitized env output: %q", joined)
	}
}

func TestApplyFortConfigSet(t *testing.T) {
	settings := defaultSettings()
	changed, err := applyFortConfigSet(&settings, fortConfigSetInput{
		RepoProvided:          true,
		Repo:                  "/tmp/fort",
		BinProvided:           true,
		Bin:                   "/tmp/fort/bin/fort",
		HostProvided:          true,
		Host:                  "https://fort.example.test",
		ContainerHostProvided: true,
		ContainerHost:         "https://fort.internal.example.test",
		BuildProvided:         true,
		BuildRaw:              "true",
	})
	if err != nil {
		t.Fatalf("applyFortConfigSet: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if settings.Fort.Repo != "/tmp/fort" || settings.Fort.Bin != "/tmp/fort/bin/fort" {
		t.Fatalf("unexpected fort repo/bin: %#v", settings.Fort)
	}
	if settings.Fort.Host != "https://fort.example.test" {
		t.Fatalf("unexpected fort host: %#v", settings.Fort)
	}
	if settings.Fort.ContainerHost != "https://fort.internal.example.test" {
		t.Fatalf("unexpected fort container host: %#v", settings.Fort)
	}
	if settings.Fort.Build == nil || !*settings.Fort.Build {
		t.Fatalf("expected fort.build=true")
	}
}

func TestApplyFortConfigSetClearsBuildOnEmpty(t *testing.T) {
	settings := defaultSettings()
	settings.Fort.Build = boolPtr(true)
	changed, err := applyFortConfigSet(&settings, fortConfigSetInput{BuildProvided: true, BuildRaw: ""})
	if err != nil {
		t.Fatalf("applyFortConfigSet: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if settings.Fort.Build != nil {
		t.Fatalf("expected build unset")
	}
}

func TestApplyFortConfigSetRejectsInvalidBuild(t *testing.T) {
	settings := defaultSettings()
	_, err := applyFortConfigSet(&settings, fortConfigSetInput{BuildProvided: true, BuildRaw: "bad"})
	if err == nil {
		t.Fatalf("expected invalid build error")
	}
}

func TestDefaultFortRepoPathFindsSiblingRepo(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		_ = os.Chdir(orig)
	}()

	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "fort"), 0o755); err != nil {
		t.Fatalf("mkdir fort sibling: %v", err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatalf("chdir base: %v", err)
	}

	got := defaultFortRepoPath()
	want := filepath.Join(base, "fort")
	if got != want {
		t.Fatalf("defaultFortRepoPath()=%q want=%q", got, want)
	}
}

func TestDefaultFortRepoPathEmptyWhenMissing(t *testing.T) {
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

	got := defaultFortRepoPath()
	if got != "" {
		t.Fatalf("defaultFortRepoPath()=%q want empty", got)
	}
}

func TestFortConfigPersistsAcrossSaveLoad(t *testing.T) {
	t.Setenv("SI_SETTINGS_HOME", t.TempDir())

	settings := loadSettingsOrDefault()
	changed, err := applyFortConfigSet(&settings, fortConfigSetInput{
		RepoProvided:          true,
		Repo:                  "/tmp/fort",
		BinProvided:           true,
		Bin:                   "/tmp/fort/bin/fort",
		HostProvided:          true,
		Host:                  "https://fort.example.test",
		ContainerHostProvided: true,
		ContainerHost:         "https://fort.internal.example.test",
		BuildProvided:         true,
		BuildRaw:              "true",
	})
	if err != nil {
		t.Fatalf("applyFortConfigSet: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if err := saveSettings(settings); err != nil {
		t.Fatalf("saveSettings: %v", err)
	}

	loaded := loadSettingsOrDefault()
	if loaded.Fort.Repo != "/tmp/fort" {
		t.Fatalf("unexpected loaded fort repo: %q", loaded.Fort.Repo)
	}
	if loaded.Fort.Bin != "/tmp/fort/bin/fort" {
		t.Fatalf("unexpected loaded fort bin: %q", loaded.Fort.Bin)
	}
	if loaded.Fort.Host != "https://fort.example.test" {
		t.Fatalf("unexpected loaded fort host: %q", loaded.Fort.Host)
	}
	if loaded.Fort.ContainerHost != "https://fort.internal.example.test" {
		t.Fatalf("unexpected loaded fort container host: %q", loaded.Fort.ContainerHost)
	}
	if loaded.Fort.Build == nil || !*loaded.Fort.Build {
		t.Fatalf("expected loaded fort.build=true")
	}
}
