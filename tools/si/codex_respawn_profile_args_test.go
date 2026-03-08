package main

import "testing"

func TestNormalizeRespawnSpawnProfileArgs_InfersProfileFromContainerName(t *testing.T) {
	filtered, profile := normalizeRespawnSpawnProfileArgs(
		[]string{"--detach=false"},
		"profile-alpha",
		"",
		func(name string) (string, bool) {
			if name == "profile-alpha" {
				return "profile-alpha", true
			}
			return "", false
		},
	)
	if profile != "profile-alpha" {
		t.Fatalf("unexpected profile %q", profile)
	}
	want := []string{"--detach=false", "--profile", "profile-alpha"}
	if len(filtered) != len(want) {
		t.Fatalf("unexpected filtered len=%d want=%d (%v)", len(filtered), len(want), filtered)
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("unexpected filtered[%d]=%q want=%q", i, filtered[i], want[i])
		}
	}
}

func TestNormalizeRespawnSpawnProfileArgs_DisablesDefaultProfileWhenNoProfileResolved(t *testing.T) {
	filtered, profile := normalizeRespawnSpawnProfileArgs(
		[]string{"--detach=false"},
		"custom",
		"",
		func(string) (string, bool) { return "", false },
	)
	if profile != "" {
		t.Fatalf("expected empty profile, got %q", profile)
	}
	want := []string{"--detach=false", "--profile="}
	if len(filtered) != len(want) {
		t.Fatalf("unexpected filtered len=%d want=%d (%v)", len(filtered), len(want), filtered)
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("unexpected filtered[%d]=%q want=%q", i, filtered[i], want[i])
		}
	}
}

func TestNormalizeRespawnSpawnProfileArgs_PreservesExplicitProfileFlag(t *testing.T) {
	filtered, profile := normalizeRespawnSpawnProfileArgs(
		[]string{"--profile", "profile-gamma"},
		"profile-gamma",
		"profile-gamma",
		func(string) (string, bool) { return "", false },
	)
	if profile != "profile-gamma" {
		t.Fatalf("unexpected profile %q", profile)
	}
	want := []string{"--profile", "profile-gamma"}
	if len(filtered) != len(want) {
		t.Fatalf("unexpected filtered len=%d want=%d (%v)", len(filtered), len(want), filtered)
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("unexpected filtered[%d]=%q want=%q", i, filtered[i], want[i])
		}
	}
}
