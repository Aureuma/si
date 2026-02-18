package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSyncCodexSkillsCopiesBundleIntoCodexAndShared(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(home, ".codex")
	if err := os.MkdirAll(filepath.Join(home, ".si"), 0o700); err != nil {
		t.Fatalf("mkdir .si: %v", err)
	}
	bundle := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(filepath.Join(bundle, "si-vault-ops"), 0o755); err != nil {
		t.Fatalf("mkdir bundle skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "si-vault-ops", "SKILL.md"), []byte("name: si-vault-ops"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(bundle, "si-vault-ops", "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	execPath := filepath.Join(bundle, "si-vault-ops", "scripts", "check.sh")
	if err := os.WriteFile(execPath, []byte("#!/usr/bin/env bash\necho ok\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv("SI_CODEX_SKILLS_BUNDLE_DIR", bundle)
	if err := syncCodexSkills(home, codexHome); err != nil {
		t.Fatalf("syncCodexSkills: %v", err)
	}

	// Codex target
	targetSkill := filepath.Join(codexHome, "skills", "si-vault-ops", "SKILL.md")
	if _, err := os.Stat(targetSkill); err != nil {
		t.Fatalf("missing target skill file: %v", err)
	}
	targetScript := filepath.Join(codexHome, "skills", "si-vault-ops", "scripts", "check.sh")
	info, err := os.Stat(targetScript)
	if err != nil {
		t.Fatalf("missing target script: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected executable mode for copied script, got %v", info.Mode())
	}

	// Shared mirror under ~/.si
	sharedSkill := filepath.Join(home, ".si", "codex", "skills", "si-vault-ops", "SKILL.md")
	if _, err := os.Stat(sharedSkill); err != nil {
		t.Fatalf("missing shared skill file: %v", err)
	}
}

func TestSyncCodexSkillsMissingBundleReturnsNil(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(home, ".codex")
	t.Setenv("SI_CODEX_SKILLS_BUNDLE_DIR", filepath.Join(t.TempDir(), "missing-skills"))
	if err := syncCodexSkills(home, codexHome); err != nil {
		t.Fatalf("expected nil error for missing bundle, got: %v", err)
	}
	// Target dir should still exist because codex-init creates it before syncing.
	if _, err := os.Stat(filepath.Join(codexHome, "skills")); err != nil {
		t.Fatalf("expected codex skills dir to exist: %v", err)
	}
}

func TestParseArgsExecForwarding(t *testing.T) {
	quiet, execArgs := parseArgs([]string{"--quiet", "--exec", "bash", "-lc", "echo hi"})
	if !quiet {
		t.Fatalf("expected quiet=true")
	}
	got := strings.Join(execArgs, " ")
	if got != "bash -lc echo hi" {
		t.Fatalf("unexpected exec args: %q", got)
	}
}

func TestDecodeMountInfoPath(t *testing.T) {
	in := `/home/si/.si\040with\040space\011tab\012line\134slash`
	got := decodeMountInfoPath(in)
	want := "/home/si/.si with space\ttab\nline\\slash"
	if got != want {
		t.Fatalf("decodeMountInfoPath()=%q want=%q", got, want)
	}
}

func TestCollectGitSafeDirectoriesIncludesDevelopmentChildren(t *testing.T) {
	base := t.TempDir()
	development := filepath.Join(base, "home", "shawn", "Development")
	viva := filepath.Join(development, "viva")
	si := filepath.Join(development, "si")

	mustMakeGitRepoRoot(t, viva)
	mustMakeGitRepoRoot(t, si)

	got := collectGitSafeDirectories([]string{development}, viva)
	want := []string{si, viva}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectGitSafeDirectories()=%v want=%v", got, want)
	}
}

func TestCollectGitSafeDirectoriesIncludesMountAndCwdRepos(t *testing.T) {
	base := t.TempDir()
	mounted := filepath.Join(base, "workspace", "repo")
	cwd := filepath.Join(base, "home", "si", "current")

	mustMakeGitRepoRoot(t, mounted)
	mustMakeGitRepoRoot(t, cwd)

	got := collectGitSafeDirectories([]string{mounted}, cwd)
	want := []string{cwd, mounted}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectGitSafeDirectories()=%v want=%v", got, want)
	}
}

func TestCollectGitSafeDirectoriesDeduplicatesEntries(t *testing.T) {
	base := t.TempDir()
	development := filepath.Join(base, "home", "shawn", "Development")
	viva := filepath.Join(development, "viva")
	mustMakeGitRepoRoot(t, viva)

	got := collectGitSafeDirectories([]string{development, viva, viva}, viva)
	want := []string{viva}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectGitSafeDirectories()=%v want=%v", got, want)
	}
}

func mustMakeGitRepoRoot(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir repo root %q: %v", path, err)
	}
	if err := os.MkdirAll(filepath.Join(path, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git for %q: %v", path, err)
	}
}

func TestBrowserMCPURLFromEnvDefaults(t *testing.T) {
	t.Setenv("SI_BROWSER_MCP_DISABLED", "")
	t.Setenv("SI_BROWSER_MCP_URL_INTERNAL", "")
	t.Setenv("SI_BROWSER_MCP_URL", "")
	t.Setenv("SI_BROWSER_CONTAINER", "")
	t.Setenv("SI_BROWSER_MCP_PORT", "")
	got := browserMCPURLFromEnv()
	want := "http://si-playwright-mcp-headed:8931/mcp"
	if got != want {
		t.Fatalf("browserMCPURLFromEnv()=%q want=%q", got, want)
	}
}

func TestBrowserMCPURLFromEnvUsesInternalOverride(t *testing.T) {
	t.Setenv("SI_BROWSER_MCP_DISABLED", "")
	t.Setenv("SI_BROWSER_MCP_URL_INTERNAL", "http://custom-browser:9999/mcp")
	got := browserMCPURLFromEnv()
	want := "http://custom-browser:9999/mcp"
	if got != want {
		t.Fatalf("browserMCPURLFromEnv()=%q want=%q", got, want)
	}
}

func TestBrowserMCPURLFromEnvDisabled(t *testing.T) {
	t.Setenv("SI_BROWSER_MCP_DISABLED", "true")
	if got := browserMCPURLFromEnv(); got != "" {
		t.Fatalf("expected empty browser MCP URL when disabled, got %q", got)
	}
}
