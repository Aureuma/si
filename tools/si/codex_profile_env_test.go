package main

import "testing"

func TestResolveContainerProfileEnvUsesExplicitProfile(t *testing.T) {
	t.Setenv("SI_CODEX_PROFILE_ID", "host-id")
	t.Setenv("SI_CODEX_PROFILE_NAME", "Host Name")

	id, name := resolveContainerProfileEnv(&codexProfile{
		ID:   "profile-alpha",
		Name: "Profile Alpha",
	})
	if id != "profile-alpha" || name != "Profile Alpha" {
		t.Fatalf("unexpected profile env: id=%q name=%q", id, name)
	}
}

func TestResolveContainerProfileEnvFallsBackToHostEnv(t *testing.T) {
	t.Setenv("SI_CODEX_PROFILE_ID", "host-id")
	t.Setenv("SI_CODEX_PROFILE_NAME", "Host Name")

	id, name := resolveContainerProfileEnv(nil)
	if id != "host-id" || name != "Host Name" {
		t.Fatalf("unexpected fallback env: id=%q name=%q", id, name)
	}
}

func TestAppendContainerProfileEnvValues(t *testing.T) {
	got := appendContainerProfileEnvValues([]string{"HOME=/home/si"}, "profile-gamma", "Profile Gamma")
	if len(got) != 3 {
		t.Fatalf("unexpected env len: %d (%v)", len(got), got)
	}
	if got[1] != "SI_CODEX_PROFILE_ID=profile-gamma" {
		t.Fatalf("missing profile id env: %v", got)
	}
	if got[2] != "SI_CODEX_PROFILE_NAME=Profile Gamma" {
		t.Fatalf("missing profile name env: %v", got)
	}
}
