package main

import (
	"path/filepath"
	"testing"
)

func TestResolveReleasemindPlannerRepoExplicit(t *testing.T) {
	t.Setenv("SI_RELEASEMIND_REPO", "")
	tmp := t.TempDir()
	got, err := resolveReleasemindPlannerRepo(tmp)
	if err != nil {
		t.Fatalf("resolveReleasemindPlannerRepo returned error: %v", err)
	}
	want, err := filepath.Abs(tmp)
	if err != nil {
		t.Fatalf("filepath.Abs failed: %v", err)
	}
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestResolveReleasemindPlannerRepoFromEnv(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SI_RELEASEMIND_REPO", tmp)
	got, err := resolveReleasemindPlannerRepo("")
	if err != nil {
		t.Fatalf("resolveReleasemindPlannerRepo returned error: %v", err)
	}
	want, err := filepath.Abs(tmp)
	if err != nil {
		t.Fatalf("filepath.Abs failed: %v", err)
	}
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
