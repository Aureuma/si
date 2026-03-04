package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyFortConfigSet(t *testing.T) {
	settings := defaultSettings()
	changed, err := applyFortConfigSet(&settings, fortConfigSetInput{
		RepoProvided:  true,
		Repo:          "/tmp/fort",
		BinProvided:   true,
		Bin:           "/tmp/fort/bin/fort",
		BuildProvided: true,
		BuildRaw:      "true",
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
		RepoProvided:  true,
		Repo:          "/tmp/fort",
		BinProvided:   true,
		Bin:           "/tmp/fort/bin/fort",
		BuildProvided: true,
		BuildRaw:      "true",
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
	if loaded.Fort.Build == nil || !*loaded.Fort.Build {
		t.Fatalf("expected loaded fort.build=true")
	}
}
