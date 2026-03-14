package main

import (
	"os"
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

func TestResolveReleasemindPlannerRepoFromSiblingDirectory(t *testing.T) {
	t.Setenv("SI_RELEASEMIND_REPO", "")
	base := t.TempDir()
	workDir := filepath.Join(base, "workspace")
	plannerDir := filepath.Join(base, "releasemind", "engine", "playrelease")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir work dir: %v", err)
	}
	if err := os.MkdirAll(plannerDir, 0o755); err != nil {
		t.Fatalf("mkdir planner dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plannerDir, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		_ = os.Chdir(origWD)
	}()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("chdir work dir: %v", err)
	}

	got, err := resolveReleasemindPlannerRepo("")
	if err != nil {
		t.Fatalf("resolveReleasemindPlannerRepo returned error: %v", err)
	}
	want, err := filepath.Abs(filepath.Join(base, "releasemind"))
	if err != nil {
		t.Fatalf("filepath.Abs failed: %v", err)
	}
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
