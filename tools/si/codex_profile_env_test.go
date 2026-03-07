package main

import "testing"

func TestResolveContainerProfileEnvUsesExplicitProfile(t *testing.T) {
	t.Setenv("SI_CODEX_PROFILE_ID", "host-id")
	t.Setenv("SI_CODEX_PROFILE_NAME", "Host Name")

	id, name := resolveContainerProfileEnv(&codexProfile{
		ID:   "america",
		Name: "America",
	})
	if id != "america" || name != "America" {
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
	got := appendContainerProfileEnvValues([]string{"HOME=/home/si"}, "cadma", "Cadma")
	if len(got) != 3 {
		t.Fatalf("unexpected env len: %d (%v)", len(got), got)
	}
	if got[1] != "SI_CODEX_PROFILE_ID=cadma" {
		t.Fatalf("missing profile id env: %v", got)
	}
	if got[2] != "SI_CODEX_PROFILE_NAME=Cadma" {
		t.Fatalf("missing profile name env: %v", got)
	}
}
