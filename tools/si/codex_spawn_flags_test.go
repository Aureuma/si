package main

import "testing"

func TestCodexSpawnBoolFlags(t *testing.T) {
	flags := codexSpawnBoolFlags()
	if !flags["detach"] {
		t.Fatalf("expected detach bool flag")
	}
	if !flags["clean-slate"] {
		t.Fatalf("expected clean-slate bool flag")
	}
	if !flags["docker-socket"] {
		t.Fatalf("expected docker-socket bool flag")
	}
}

func TestCodexRespawnBoolFlagsIncludesVolumes(t *testing.T) {
	flags := codexRespawnBoolFlags()
	if !flags["volumes"] {
		t.Fatalf("expected volumes bool flag")
	}
	if !flags["detach"] || !flags["docker-socket"] || !flags["clean-slate"] {
		t.Fatalf("expected respawn bool flags to include spawn bool flags: %#v", flags)
	}
}

func TestSplitNameAndFlagsParsesBoolWithSeparateValue(t *testing.T) {
	name, filtered := splitNameAndFlags([]string{
		"--detach", "false",
		"--docker-socket", "true",
		"cadma",
		"--repo", "acme/repo",
	}, codexSpawnBoolFlags())

	if name != "cadma" {
		t.Fatalf("unexpected container name %q", name)
	}

	want := []string{
		"--detach=false",
		"--docker-socket=true",
		"--repo", "acme/repo",
	}
	if len(filtered) != len(want) {
		t.Fatalf("unexpected filtered len=%d want=%d (%v)", len(filtered), len(want), filtered)
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("unexpected filtered[%d]=%q want %q (%v)", i, filtered[i], want[i], filtered)
		}
	}
}

func TestSplitNameAndFlagsIgnoresBoolWithoutLiteralValue(t *testing.T) {
	name, filtered := splitNameAndFlags([]string{
		"einsteina",
		"--detach",
		"--repo", "acme/repo",
	}, codexSpawnBoolFlags())
	if name != "einsteina" {
		t.Fatalf("unexpected container name %q", name)
	}
	want := []string{"--detach", "--repo", "acme/repo"}
	if len(filtered) != len(want) {
		t.Fatalf("unexpected filtered len=%d want=%d (%v)", len(filtered), len(want), filtered)
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("unexpected filtered[%d]=%q want %q (%v)", i, filtered[i], want[i], filtered)
		}
	}
}

