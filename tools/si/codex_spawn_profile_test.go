package main

import "testing"

func TestCodexDefaultProfileKey(t *testing.T) {
	settings := Settings{}
	if got := codexDefaultProfileKey(settings); got != "" {
		t.Fatalf("expected empty key, got %q", got)
	}

	settings.Codex.Profile = "profile-gamma"
	settings.Codex.Profiles.Active = "profile-beta"
	if got := codexDefaultProfileKey(settings); got != "profile-gamma" {
		t.Fatalf("expected codex.profile to win, got %q", got)
	}

	settings.Codex.Profile = ""
	if got := codexDefaultProfileKey(settings); got != "profile-beta" {
		t.Fatalf("expected active profile fallback, got %q", got)
	}
}

func TestChoosePreferredCodexContainer(t *testing.T) {
	items := []codexProfileContainerRef{
		{Name: "si-codex-legacy1", State: "exited"},
		{Name: "si-codex-legacy2", State: "running"},
	}
	got := choosePreferredCodexContainer(items, "")
	if got.Name != "si-codex-legacy2" {
		t.Fatalf("expected running container, got %q", got.Name)
	}

	got = choosePreferredCodexContainer(items, "si-codex-legacy1")
	if got.Name != "si-codex-legacy1" {
		t.Fatalf("expected preferred container, got %q", got.Name)
	}
}
